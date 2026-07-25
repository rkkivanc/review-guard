package domain

// FinetuneChatMessage is one turn in a supervised chat example.
type FinetuneChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// FinetuneExample is one human-feedback supervised sample for PEFT/LoRA.
type FinetuneExample struct {
	ReviewID string                `json:"review_id"`
	GameName string                `json:"game_name"`
	Stars    int                   `json:"stars"`
	Messages []FinetuneChatMessage `json:"messages"`
}

// FinetuneExport is the admin export payload.
type FinetuneExport struct {
	Count    int               `json:"count"`
	Examples []FinetuneExample `json:"examples"`
}
