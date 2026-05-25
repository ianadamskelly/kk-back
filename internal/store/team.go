package store

import "context"

// TeamMember is a person shown on the About page.
type TeamMember struct {
	ID        int64             `json:"id" db:"id"`
	Name      string            `json:"name" db:"name"`
	Role      string            `json:"role" db:"role"`
	Photo     string            `json:"photo" db:"photo"`
	Bio       string            `json:"bio" db:"bio"`
	Socials   map[string]string `json:"socials" db:"socials"`
	SortOrder int               `json:"sortOrder" db:"sort_order"`
}

const teamSelect = `SELECT id, name, role, photo, bio, socials, sort_order FROM team_members`

// ListTeam returns team members ordered for display.
func (s *Store) ListTeam(ctx context.Context) ([]TeamMember, error) {
	return queryRows[TeamMember](ctx, s.pool, teamSelect+` ORDER BY sort_order, name`)
}

// GetTeamMember returns one team member by id.
func (s *Store) GetTeamMember(ctx context.Context, id int64) (*TeamMember, error) {
	return queryOne[TeamMember](ctx, s.pool, teamSelect+` WHERE id = $1`, id)
}

// CreateTeamMember inserts a new team member.
func (s *Store) CreateTeamMember(ctx context.Context, m *TeamMember) error {
	if m.Socials == nil {
		m.Socials = map[string]string{}
	}
	return s.pool.QueryRow(ctx, `
		INSERT INTO team_members (name, role, photo, bio, socials, sort_order)
		VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
		m.Name, m.Role, m.Photo, m.Bio, m.Socials, m.SortOrder,
	).Scan(&m.ID)
}

// UpdateTeamMember saves changes to an existing team member.
func (s *Store) UpdateTeamMember(ctx context.Context, m *TeamMember) error {
	if m.Socials == nil {
		m.Socials = map[string]string{}
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE team_members
		SET name=$1, role=$2, photo=$3, bio=$4, socials=$5, sort_order=$6
		WHERE id=$7`,
		m.Name, m.Role, m.Photo, m.Bio, m.Socials, m.SortOrder, m.ID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteTeamMember removes a team member by id.
func (s *Store) DeleteTeamMember(ctx context.Context, id int64) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM team_members WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
