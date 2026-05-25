package store

// PermissionDef describes one permission the admin can grant to a role.
// Permissions follow the pattern "{resource}.{action}" — action is either
// "view" (read-only) or "manage" (full CRUD).
type PermissionDef struct {
	Key         string `json:"key"`
	Resource    string `json:"resource"`
	Action      string `json:"action"`     // "view" | "manage" | "invite"
	Label       string `json:"label"`      // human-readable for the UI
	Description string `json:"description"`
}

// ResourceGroup bundles the view+manage permissions of one resource so the
// admin UI can render a clean two-column matrix.
type ResourceGroup struct {
	Resource    string   `json:"resource"`
	Label       string   `json:"label"`
	Category    string   `json:"category"`
	Permissions []string `json:"permissions"`
}

// resourceMeta is the source-of-truth list. Adding a new admin area is a
// matter of one entry here plus middleware on the route(s).
var resourceMeta = []struct {
	Resource string
	Label    string
	Category string
}{
	// Content
	{"posts", "Posts / Insights", "Content"},
	{"categories", "Categories", "Content"},
	{"comments", "Comments", "Content"},
	// Site
	{"services", "Services", "Site"},
	{"projects", "Projects", "Site"},
	{"team", "Team", "Site"},
	{"testimonials", "Testimonials", "Site"},
	{"stats", "Stats", "Site"},
	{"settings", "Site settings", "Site"},
	{"submissions", "Contact messages", "Site"},
	{"subscribers", "Newsletter subscribers", "Site"},
	{"newsletters", "Newsletter campaigns", "Site"},
	{"tickets", "Support tickets", "Site"},
	// Shop
	{"products", "Products", "Shop"},
	{"orders", "Orders", "Shop"},
	// Learn
	{"courses", "Courses & lessons", "Learn"},
	{"library", "Library", "Learn"},
	{"memberships", "Members", "Learn"},
	// Revenue
	{"service_revenue", "Service income", "Revenue"},
	{"coupons", "Coupons", "Revenue"},
	{"rewards", "Referrals & store credit", "Revenue"},
	// Admin team
	{"users", "Staff users", "Administration"},
	{"roles", "Roles & permissions", "Administration"},
}

// AllPermissions returns every permission key the system understands.
// "users.invite" exists in addition to users.manage so an admin can grant
// invite-only access without full user-row delete rights.
func AllPermissions() []PermissionDef {
	out := make([]PermissionDef, 0, len(resourceMeta)*2+1)
	for _, r := range resourceMeta {
		out = append(out,
			PermissionDef{
				Key:         r.Resource + ".view",
				Resource:    r.Resource,
				Action:      "view",
				Label:       "View " + r.Label,
				Description: "See " + r.Label + " in the admin",
			},
			PermissionDef{
				Key:         r.Resource + ".manage",
				Resource:    r.Resource,
				Action:      "manage",
				Label:       "Manage " + r.Label,
				Description: "Create, edit, and delete " + r.Label,
			},
		)
	}
	out = append(out, PermissionDef{
		Key:         "users.invite",
		Resource:    "users",
		Action:      "invite",
		Label:       "Invite staff",
		Description: "Send invitations to add new staff users",
	})
	return out
}

// PermissionResources returns the resource catalog with the permission keys
// belonging to each. Used by the admin UI to render the matrix.
func PermissionResources() []ResourceGroup {
	out := make([]ResourceGroup, 0, len(resourceMeta))
	for _, r := range resourceMeta {
		perms := []string{r.Resource + ".view", r.Resource + ".manage"}
		if r.Resource == "users" {
			perms = append(perms, "users.invite")
		}
		out = append(out, ResourceGroup{
			Resource:    r.Resource,
			Label:       r.Label,
			Category:    r.Category,
			Permissions: perms,
		})
	}
	return out
}

// IsValidPermission tells whether the given key is one of the defined ones.
// Guards the role-create/update API from storing arbitrary garbage.
func IsValidPermission(key string) bool {
	for _, p := range AllPermissions() {
		if p.Key == key {
			return true
		}
	}
	return false
}

// AllPermissionKeys returns just the keys — used to give the Admin role
// every permission at seed time.
func AllPermissionKeys() []string {
	defs := AllPermissions()
	keys := make([]string, len(defs))
	for i, d := range defs {
		keys[i] = d.Key
	}
	return keys
}

// BuiltinRoleSeed is what we insert on first boot. The admin role is special
// (gets every permission, can't be edited or deleted, can't be unassigned
// from the last admin). Editor/Moderator/Teacher are sensible starting
// points the admin can clone or tweak.
type BuiltinRoleSeed struct {
	Key         string
	Name        string
	Description string
	Permissions []string // ignored for the admin role — it gets everything
}

// BuiltinRoles is the seed list. Order matters only for display.
func BuiltinRoles() []BuiltinRoleSeed {
	return []BuiltinRoleSeed{
		{
			Key:         "admin",
			Name:        "Admin",
			Description: "Full access to every part of the admin panel. Built-in and cannot be edited.",
			Permissions: AllPermissionKeys(),
		},
		{
			Key:         "editor",
			Name:        "Editor",
			Description: "Manages public content: posts, services, projects, team, testimonials, and shop products.",
			Permissions: []string{
				"posts.view", "posts.manage",
				"categories.view", "categories.manage",
				"services.view", "services.manage",
				"projects.view", "projects.manage",
				"team.view", "team.manage",
				"testimonials.view", "testimonials.manage",
				"stats.view", "stats.manage",
				"library.view", "library.manage",
				"products.view", "products.manage",
				"coupons.view", "coupons.manage",
			},
		},
		{
			Key:         "moderator",
			Name:        "Moderator",
			Description: "Handles community moderation, contact inbox, and newsletter campaigns.",
			Permissions: []string{
				"comments.view", "comments.manage",
				"submissions.view", "submissions.manage",
				"subscribers.view",
				"newsletters.view", "newsletters.manage",
				"tickets.view", "tickets.manage",
				"testimonials.view", "testimonials.manage",
			},
		},
		{
			Key:         "teacher",
			Name:        "Teacher",
			Description: "Builds and maintains courses, lessons, library, and sees member status.",
			Permissions: []string{
				"courses.view", "courses.manage",
				"library.view", "library.manage",
				"memberships.view",
			},
		},
	}
}
