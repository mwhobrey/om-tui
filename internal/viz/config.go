package viz

// VizConfig holds all configuration for rendering a relationship visualization.
type VizConfig struct {
	// People
	Person1 string // e.g. "Alice"
	Person2 string // e.g. "Bob"

	// Colors (CSS variable values)
	PrimaryColor    string // e.g. "#be123c" (rose)
	SecondaryColor  string // e.g. "#d97706" (amber)
	AccentColor     string // e.g. "#fbbf24"
	BackgroundColor string // e.g. "#0c0a09"

	// Timezone for heatmap display
	Timezone string // e.g. "America/New_York"

	// Password (PBKDF2-SHA256 token for client-side gate, empty = no gate).
	// Format: v1$<iterations>$<salt_hex>$<dk_hex>
	PasswordHash string

	// Section ordering (default order used if empty)
	Sections []string

	// Custom narrative content
	Interludes []Interlude // poetic breaks between sections

	// Photo gallery (interspersed between sections chronologically)
	Photos []Photo
}

// Interlude is a poetic break between data sections.
type Interlude struct {
	Text     string // prose/poetry text
	Position string // section name this interlude appears AFTER
}

// DefaultSections returns the default section ordering.
// Photos are no longer a single section — they are automatically
// interspersed between content sections via distributePhotos().
func DefaultSections() []string {
	return []string{
		"password-gate",
		"hero",
		"timeline-nav",
		"chapter-early",
		"volume-chart",
		"chapter-middle",
		"sender-split",
		"heatmap",
		"chapter-late",
		"phrases",
		"silence",
		"closing",
	}
}
