package common

// QuestionOption is one selectable answer shown to the user.
type QuestionOption struct {
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

// QuestionItem describes one question in a tool request.
type QuestionItem struct {
	Header   string           `json:"header,omitempty"`
	Question string           `json:"question"`
	Options  []QuestionOption `json:"options"`
	Multiple bool             `json:"multiple,omitempty"`
	Custom   bool             `json:"custom,omitempty"`
}

// QuestionReply carries one answer array per question.
type QuestionReply struct {
	Answers  [][]string
	Rejected bool
}

// QuestionRequest is sent to UI clients when the agent needs user input.
type QuestionRequest struct {
	Questions []QuestionItem
	Response  chan QuestionReply
}

// NewQuestionRequest creates a QuestionRequest with an initialized response channel.
func NewQuestionRequest(questions []QuestionItem) QuestionRequest {
	return QuestionRequest{
		Questions: questions,
		Response:  make(chan QuestionReply, 1),
	}
}

// QuestionFunc asks the user one or more questions and returns the selected answers.
type QuestionFunc func(req QuestionRequest) QuestionReply
