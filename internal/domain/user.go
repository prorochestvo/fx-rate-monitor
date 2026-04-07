package domain

type UserType string

const (
	UserTypeTelegram UserType = "telegrambot"
)

type User struct {
	UserType UserType
	UserID   string
}
