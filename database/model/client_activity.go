package model

// ClientActivity stores one Xray access-log derived row for clients with activity capture enabled.
type ClientActivity struct {
	Id          int64  `json:"id" gorm:"primaryKey;autoIncrement"`
	ClientEmail string `json:"clientEmail" gorm:"size:255;index:idx_client_activity_email_ts,priority:1"`
	Ts          int64  `json:"ts" gorm:"index:idx_client_activity_email_ts,priority:2"`
	FromAddr    string `json:"fromAddr" gorm:"size:255"`
	ToAddr      string `json:"toAddr" gorm:"size:512"`
	InboundTag  string `json:"inboundTag" gorm:"size:128"`
	OutboundTag string `json:"outboundTag" gorm:"size:128"`
	Event       string `json:"event" gorm:"size:32"`
}
