package domain

import "time"

// RateUserProfile carries user-scoped preferences that are not specific to any
// single subscription.
//
// Timezone is an IANA name resolvable by time.LoadLocation (e.g. "Asia/Almaty",
// "Europe/Moscow", "UTC"). Validation lives in the repository on write; readers
// may rely on the stored value being valid at the moment it was persisted but
// should still fall back gracefully if a later Go version drops a previously-
// known zone.
//
// Locale is a BCP-47 tag (e.g. "ru-RU", "kk-KZ", "en-US"). Stored as-is from
// the client; no server-side BCP-47 validation. Empty string when the client
// didn't provide one. By policy this is the only identity-adjacent field we
// keep besides chat_id — username/display-name fields are off-limits; see the
// project's no-PII feedback memory.
type RateUserProfile struct {
	UserType  UserType
	UserID    string
	Timezone  string
	Locale    string
	UpdatedAt time.Time
	CreatedAt time.Time
}
