package store

import (
	"context"
	"time"
)

// User is an authenticated account. RoleID points at the new roles table
// (the source of truth for staff permissions); Role is kept as a textual
// fall-back used by customer accounts and the JWT claims. Profile fields
// (phone, address) live here so they can pre-fill checkout and certificate
// generation without a separate "profile" table.
type User struct {
	ID                 int64      `json:"id" db:"id"`
	Email              string     `json:"email" db:"email"`
	PasswordHash       string     `json:"-" db:"password_hash"`
	Name               string     `json:"name" db:"name"`
	Role               string     `json:"role" db:"role"`
	RoleID             *int64     `json:"roleId" db:"role_id"`
	Phone              string     `json:"phone" db:"phone"`
	AddressLine1       string     `json:"addressLine1" db:"address_line1"`
	AddressLine2       string     `json:"addressLine2" db:"address_line2"`
	City               string     `json:"city" db:"city"`
	State              string     `json:"state" db:"state"`
	Country            string     `json:"country" db:"country"`
	PostalCode         string     `json:"postalCode" db:"postal_code"`
	Avatar             string     `json:"avatar" db:"avatar"`
	ReferralCode       *string    `json:"referralCode" db:"referral_code"`
	ReferredByUserID   *int64     `json:"referredByUserId" db:"referred_by_user_id"`
	ReferralRewardedAt *time.Time `json:"referralRewardedAt" db:"referral_rewarded_at"`
	CreatedAt          time.Time  `json:"createdAt" db:"created_at"`
}

const userSelect = `SELECT id, email, password_hash, name, role, role_id, phone, address_line1, address_line2, city, state, country, postal_code, avatar, referral_code, referred_by_user_id, referral_rewarded_at, created_at FROM users`

// GetUserByEmail looks up an account by its email address.
func (s *Store) GetUserByEmail(ctx context.Context, email string) (*User, error) {
	return queryOne[User](ctx, s.pool, userSelect+` WHERE email = $1`, email)
}

// GetUserByID looks up an account by its numeric id.
func (s *Store) GetUserByID(ctx context.Context, id int64) (*User, error) {
	return queryOne[User](ctx, s.pool, userSelect+` WHERE id = $1`, id)
}

// CreateUser inserts an account with a pre-hashed password.
func (s *Store) CreateUser(ctx context.Context, u *User) error {
	if u.Role == "" {
		u.Role = "customer"
	}
	return s.pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash, name, role, role_id)
		 VALUES ($1, $2, $3, $4, $5) RETURNING id, created_at`,
		u.Email, u.PasswordHash, u.Name, u.Role, u.RoleID,
	).Scan(&u.ID, &u.CreatedAt)
}

// CountUsers returns the total number of accounts.
func (s *Store) CountUsers(ctx context.Context) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

// SetUserRole reassigns the role of an existing user, keeping the legacy
// text column in sync so JWTs continue to work.
func (s *Store) SetUserRole(ctx context.Context, userID, roleID int64, roleKey string) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE users SET role_id = $1, role = $2 WHERE id = $3`,
		roleID, roleKey, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// SetUserPassword updates the bcrypt-hashed password for a user.
func (s *Store) SetUserPassword(ctx context.Context, userID int64, hash string) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE users SET password_hash = $1 WHERE id = $2`, hash, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteUser removes a user. Caller is responsible for checking that this
// won't leave the system without an admin.
func (s *Store) DeleteUser(ctx context.Context, userID int64) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// StaffUser is a user with a non-customer role attached, used by the
// /admin/users listing.
type StaffUser struct {
	User
	RoleName string `json:"roleName" db:"role_name"`
	RoleKey  string `json:"roleKey" db:"role_key"`
}

// ListStaffUsers returns every user that has a role_id (i.e. is part of
// the team, not a public customer).
func (s *Store) ListStaffUsers(ctx context.Context) ([]StaffUser, error) {
	return queryRows[StaffUser](ctx, s.pool, `
		SELECT u.id, u.email, u.password_hash, u.name, u.role, u.role_id,
		       u.phone, u.address_line1, u.address_line2, u.city, u.state, u.country, u.postal_code, u.avatar,
		       u.referral_code, u.referred_by_user_id, u.referral_rewarded_at, u.created_at,
		       r.name AS role_name, r.key AS role_key
		FROM users u
		JOIN roles r ON r.id = u.role_id
		ORDER BY u.created_at DESC`,
	)
}

// UpdateProfile saves the editable profile fields for a user. Email/role
// are not touched here — those have their own admin-controlled paths.
func (s *Store) UpdateProfile(ctx context.Context, u *User) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE users SET
			name = $1, phone = $2,
			address_line1 = $3, address_line2 = $4,
			city = $5, state = $6, country = $7, postal_code = $8,
			avatar = $9
		WHERE id = $10`,
		u.Name, u.Phone,
		u.AddressLine1, u.AddressLine2,
		u.City, u.State, u.Country, u.PostalCode,
		u.Avatar, u.ID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// UserPermissions returns the permissions granted by the user's role. An
// empty slice means "no admin access at all" (e.g. a public customer).
func (s *Store) UserPermissions(ctx context.Context, userID int64) ([]string, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT rp.permission
		FROM users u
		JOIN role_permissions rp ON rp.role_id = u.role_id
		WHERE u.id = $1`, userID)
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

// GetUserByEmailOrNil returns the first admin account, or nil if none exists.
// Used by seed code to attribute sample blog posts.
func (s *Store) GetUserByEmailOrNil(ctx context.Context) (*User, error) {
	u, err := queryOne[User](ctx, s.pool,
		userSelect+` WHERE role = 'admin' ORDER BY id LIMIT 1`)
	if err == ErrNotFound {
		return nil, nil
	}
	return u, err
}
