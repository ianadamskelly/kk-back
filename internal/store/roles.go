package store

import (
	"context"
	"time"
)

// Role groups a set of permissions under a name the admin picks. Built-in
// roles are seeded on startup; the admin role is locked from edit/delete.
type Role struct {
	ID          int64     `json:"id" db:"id"`
	Key         string    `json:"key" db:"key"`
	Name        string    `json:"name" db:"name"`
	Description string    `json:"description" db:"description"`
	IsBuiltin   bool      `json:"isBuiltin" db:"is_builtin"`
	CreatedAt   time.Time `json:"createdAt" db:"created_at"`
	UpdatedAt   time.Time `json:"updatedAt" db:"updated_at"`
	Permissions []string  `json:"permissions" db:"-"`
	UserCount   int       `json:"userCount" db:"-"`
}

const roleSelect = `SELECT id, key, name, description, is_builtin, created_at, updated_at FROM roles`

// ListRoles returns every role with its permissions and how many users it
// is currently assigned to.
func (s *Store) ListRoles(ctx context.Context) ([]Role, error) {
	roles, err := queryRows[Role](ctx, s.pool, roleSelect+` ORDER BY is_builtin DESC, name`)
	if err != nil {
		return nil, err
	}
	if len(roles) == 0 {
		return roles, nil
	}

	ids := make([]int64, len(roles))
	for i, r := range roles {
		ids[i] = r.ID
	}

	// Permissions per role.
	rows, err := s.pool.Query(ctx,
		`SELECT role_id, permission FROM role_permissions WHERE role_id = ANY($1)`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	perms := map[int64][]string{}
	for rows.Next() {
		var rid int64
		var p string
		if err := rows.Scan(&rid, &p); err != nil {
			return nil, err
		}
		perms[rid] = append(perms[rid], p)
	}

	// User counts per role.
	counts, err := s.pool.Query(ctx,
		`SELECT role_id, COUNT(*) FROM users WHERE role_id = ANY($1) GROUP BY role_id`, ids)
	if err != nil {
		return nil, err
	}
	defer counts.Close()
	countByID := map[int64]int{}
	for counts.Next() {
		var rid int64
		var n int
		if err := counts.Scan(&rid, &n); err != nil {
			return nil, err
		}
		countByID[rid] = n
	}

	for i := range roles {
		roles[i].Permissions = perms[roles[i].ID]
		if roles[i].Permissions == nil {
			roles[i].Permissions = []string{}
		}
		roles[i].UserCount = countByID[roles[i].ID]
	}
	return roles, nil
}

// GetRoleByID returns one role with its permissions.
func (s *Store) GetRoleByID(ctx context.Context, id int64) (*Role, error) {
	role, err := queryOne[Role](ctx, s.pool, roleSelect+` WHERE id = $1`, id)
	if err != nil {
		return nil, err
	}
	perms, err := s.rolePermissions(ctx, role.ID)
	if err != nil {
		return nil, err
	}
	role.Permissions = perms
	return role, nil
}

// GetRoleByKey returns one role by its stable key (e.g. "admin"). Used by
// seed/migration code to backfill users.
func (s *Store) GetRoleByKey(ctx context.Context, key string) (*Role, error) {
	role, err := queryOne[Role](ctx, s.pool, roleSelect+` WHERE key = $1`, key)
	if err != nil {
		return nil, err
	}
	perms, err := s.rolePermissions(ctx, role.ID)
	if err != nil {
		return nil, err
	}
	role.Permissions = perms
	return role, nil
}

func (s *Store) rolePermissions(ctx context.Context, roleID int64) ([]string, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT permission FROM role_permissions WHERE role_id = $1 ORDER BY permission`, roleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

// CreateRole inserts a new (non-builtin) role and its permissions.
func (s *Store) CreateRole(ctx context.Context, r *Role) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if r.Key == "" {
		r.Key = slugify(r.Name)
	}
	r.Key, err = s.uniqueSlug(ctx, "roles", r.Key, 0)
	if err != nil {
		return err
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO roles (key, name, description, is_builtin)
		VALUES ($1, $2, $3, FALSE)
		RETURNING id, is_builtin, created_at, updated_at`,
		r.Key, r.Name, r.Description,
	).Scan(&r.ID, &r.IsBuiltin, &r.CreatedAt, &r.UpdatedAt); err != nil {
		return err
	}
	for _, p := range r.Permissions {
		if _, err := tx.Exec(ctx,
			`INSERT INTO role_permissions (role_id, permission) VALUES ($1, $2)
			 ON CONFLICT DO NOTHING`,
			r.ID, p,
		); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// UpdateRole replaces the name/description/permissions of a role. Built-in
// roles must be guarded by the caller (we do that in the API layer).
func (s *Store) UpdateRole(ctx context.Context, r *Role) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	tag, err := tx.Exec(ctx, `
		UPDATE roles SET name = $1, description = $2, updated_at = now()
		WHERE id = $3`, r.Name, r.Description, r.ID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	if _, err := tx.Exec(ctx,
		`DELETE FROM role_permissions WHERE role_id = $1`, r.ID); err != nil {
		return err
	}
	for _, p := range r.Permissions {
		if _, err := tx.Exec(ctx,
			`INSERT INTO role_permissions (role_id, permission) VALUES ($1, $2)
			 ON CONFLICT DO NOTHING`, r.ID, p); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// SetRolePermissions replaces just the permissions list. Used by the admin
// UI for built-in roles other than admin (which never reaches here).
func (s *Store) SetRolePermissions(ctx context.Context, roleID int64, perms []string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx,
		`DELETE FROM role_permissions WHERE role_id = $1`, roleID); err != nil {
		return err
	}
	for _, p := range perms {
		if _, err := tx.Exec(ctx,
			`INSERT INTO role_permissions (role_id, permission) VALUES ($1, $2)
			 ON CONFLICT DO NOTHING`, roleID, p); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx,
		`UPDATE roles SET updated_at = now() WHERE id = $1`, roleID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// DeleteRole removes a non-builtin role. Users currently assigned to it get
// their role_id nulled via the FK (ON DELETE SET NULL was not set; we
// instead require the caller to ensure no users are using it).
func (s *Store) DeleteRole(ctx context.Context, id int64) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM roles WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// UpsertBuiltinRole creates the role on first boot. On later boots it just
// ensures the row exists; permissions are seeded only when the role is new
// so admin tweaks to built-ins survive restarts.
func (s *Store) UpsertBuiltinRole(ctx context.Context, key, name, description string, permissions []string) (int64, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	var id int64
	var inserted bool
	err = tx.QueryRow(ctx, `
		INSERT INTO roles (key, name, description, is_builtin)
		VALUES ($1, $2, $3, TRUE)
		ON CONFLICT (key) DO NOTHING
		RETURNING id`,
		key, name, description,
	).Scan(&id)
	if err == nil {
		inserted = true
	} else {
		// Already exists — fetch the id.
		if err := tx.QueryRow(ctx,
			`SELECT id FROM roles WHERE key = $1`, key).Scan(&id); err != nil {
			return 0, err
		}
	}

	if inserted {
		for _, p := range permissions {
			if _, err := tx.Exec(ctx,
				`INSERT INTO role_permissions (role_id, permission) VALUES ($1, $2)
				 ON CONFLICT DO NOTHING`, id, p); err != nil {
				return 0, err
			}
		}
	} else if key == "admin" {
		// The admin role must always have every permission — re-sync on every
		// boot so new permissions added in future releases are picked up.
		for _, p := range permissions {
			if _, err := tx.Exec(ctx,
				`INSERT INTO role_permissions (role_id, permission) VALUES ($1, $2)
				 ON CONFLICT DO NOTHING`, id, p); err != nil {
				return 0, err
			}
		}
	}
	return id, tx.Commit(ctx)
}

// AdminUserCount returns how many users currently have the admin role.
// Used to refuse demoting/deleting the last admin.
func (s *Store) AdminUserCount(ctx context.Context) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM users u
		JOIN roles r ON r.id = u.role_id
		WHERE r.key = 'admin'`,
	).Scan(&n)
	return n, err
}
