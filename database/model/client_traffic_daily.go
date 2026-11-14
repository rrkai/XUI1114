package model

type ClientTrafficDaily struct {
	Id    int    `json:"id" gorm:"primaryKey;autoIncrement"`
	Email string `json:"email" gorm:"uniqueIndex:idx_client_daily_email_day"`
	Day   string `json:"day" gorm:"uniqueIndex:idx_client_daily_email_day;index"`
	Up    int64  `json:"up"`
	Down  int64  `json:"down"`
}
