package database

type (
	ChatState int

	Chat struct {
		PreviousState ChatState `json:"prev_state" binding:"required" example:"100"`
		CurrentState  ChatState `json:"curr_state" binding:"required" example:"300"`
	}
)

const (
	STATE_DUMMY     = 0
	STATE_GREETINGS = 100
	STATE_MAIN_MENU = 300
	STATE_PARTING   = 500

	STATE_PART_1 = 1000
	STATE_PART_2 = 2000
	STATE_PART_3 = 3000
	STATE_PART_4 = 4000
	STATE_PART_5 = 5000
	STATE_PART_6 = 6000
	STATE_PART_7 = 7000
	STATE_PART_8 = 8000
	STATE_PART_9 = 9000

	STATE_PART_10 = 10000
	STATE_PART_11 = 20000
	STATE_PART_12 = 30000
	STATE_PART_13 = 40000
)
