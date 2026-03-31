package quiz

import "github.com/nambuivu/quiz-server/internal/models"

// MockQuizzes provides hardcoded quiz content for the challenge demo.
// AI-ASSISTED: Claude Code — generated vocabulary quiz content
// Verification: reviewed questions for correctness, ensured each has exactly one correct answer
func MockQuizzes() map[string]*models.Quiz {
	return map[string]*models.Quiz{
		"quiz-vocab-01": {
			ID:    "quiz-vocab-01",
			Title: "English Vocabulary Challenge",
			Questions: []models.Question{
				{
					ID:   "q1",
					Text: "What does 'ubiquitous' mean?",
					Options: []models.Option{
						{ID: "a1", Text: "Rare"},
						{ID: "a2", Text: "Present everywhere"},
						{ID: "a3", Text: "Ancient"},
						{ID: "a4", Text: "Fragile"},
					},
					CorrectID: "a2",
					TimeLimit: 30,
					SortOrder: 1,
				},
				{
					ID:   "q2",
					Text: "What is a synonym for 'eloquent'?",
					Options: []models.Option{
						{ID: "a1", Text: "Clumsy"},
						{ID: "a2", Text: "Articulate"},
						{ID: "a3", Text: "Silent"},
						{ID: "a4", Text: "Aggressive"},
					},
					CorrectID: "a2",
					TimeLimit: 30,
					SortOrder: 2,
				},
				{
					ID:   "q3",
					Text: "What does 'ephemeral' mean?",
					Options: []models.Option{
						{ID: "a1", Text: "Lasting forever"},
						{ID: "a2", Text: "Very large"},
						{ID: "a3", Text: "Short-lived"},
						{ID: "a4", Text: "Extremely bright"},
					},
					CorrectID: "a3",
					TimeLimit: 30,
					SortOrder: 3,
				},
				{
					ID:   "q4",
					Text: "What is the meaning of 'pragmatic'?",
					Options: []models.Option{
						{ID: "a1", Text: "Idealistic"},
						{ID: "a2", Text: "Practical and realistic"},
						{ID: "a3", Text: "Emotional"},
						{ID: "a4", Text: "Theoretical"},
					},
					CorrectID: "a2",
					TimeLimit: 30,
					SortOrder: 4,
				},
				{
					ID:   "q5",
					Text: "What does 'benevolent' mean?",
					Options: []models.Option{
						{ID: "a1", Text: "Hostile"},
						{ID: "a2", Text: "Indifferent"},
						{ID: "a3", Text: "Well-meaning and kindly"},
						{ID: "a4", Text: "Mysterious"},
					},
					CorrectID: "a3",
					TimeLimit: 30,
					SortOrder: 5,
				},
			},
		},
	}
}
