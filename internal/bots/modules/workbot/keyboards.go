package workbot

import (
	"fmt"

	"github.com/example/bots-platform/internal/telegram"
)

func PhoneKeyboard() telegram.ReplyKeyboardMarkup {
	return telegram.ReplyKeyboardMarkup{
		Keyboard: [][]telegram.KeyboardButton{
			{{Text: "📱 Поделиться номером", RequestContact: true}},
		},
		ResizeKeyboard:  true,
		OneTimeKeyboard: true,
	}
}

func LocationKeyboard() telegram.ReplyKeyboardMarkup {
	return telegram.ReplyKeyboardMarkup{
		Keyboard: [][]telegram.KeyboardButton{
			{{Text: "📍 Поделиться геолокацией", RequestLocation: true}},
		},
		ResizeKeyboard:  true,
		OneTimeKeyboard: true,
	}
}

func ConfirmProfileKeyboard() telegram.InlineKeyboardMarkup {
	return telegram.InlineKeyboardMarkup{
		InlineKeyboard: [][]telegram.InlineKeyboardButton{
			{
				{Text: "✅ Всё верно", CallbackData: "workbot:confirm"},
				{Text: "✏️ Изменить данные", CallbackData: "workbot:edit"},
			},
		},
	}
}

// VacancyKeyboard builds dynamic join buttons based on available slots.
func VacancyKeyboard(vacancyID string, availableSlots int) telegram.InlineKeyboardMarkup {
	if availableSlots <= 0 {
		return telegram.InlineKeyboardMarkup{InlineKeyboard: [][]telegram.InlineKeyboardButton{}}
	}

	row := make([]telegram.InlineKeyboardButton, 0, availableSlots)
	for i := 1; i <= availableSlots; i++ {
		label := fmt.Sprintf("Еду %d", i)
		if i > 1 {
			label = fmt.Sprintf("Едем %d", i)
		}
		row = append(row, telegram.InlineKeyboardButton{
			Text:         label,
			CallbackData: fmt.Sprintf("workbot:apply:%s:%d", vacancyID, i),
		})
	}

	return telegram.InlineKeyboardMarkup{
		InlineKeyboard: [][]telegram.InlineKeyboardButton{row},
	}
}

func ApplyConfirmKeyboard(vacancyID string, count int) telegram.InlineKeyboardMarkup {
	return telegram.InlineKeyboardMarkup{
		InlineKeyboard: [][]telegram.InlineKeyboardButton{
			{
				{Text: "✅ Подтвердить", CallbackData: fmt.Sprintf("workbot:confirm_apply:%s:%d", vacancyID, count)},
				{Text: "❌ Отменить", CallbackData: fmt.Sprintf("workbot:cancel_apply:%s", vacancyID)},
			},
		},
	}
}

// MyJobsKeyboard builds a keyboard where each row is one vacancy with a cancel button.
func MyJobsKeyboard(vacancies []*Vacancy) telegram.InlineKeyboardMarkup {
	rows := make([][]telegram.InlineKeyboardButton, 0, len(vacancies))
	for _, v := range vacancies {
		label := fmt.Sprintf("❌ %s, %s", v.City, v.Address)
		if len(label) > 60 {
			label = label[:57] + "…"
		}
		rows = append(rows, []telegram.InlineKeyboardButton{
			{Text: label, CallbackData: fmt.Sprintf("workbot:cancel_myjob:%s", v.ID)},
		})
	}
	return telegram.InlineKeyboardMarkup{InlineKeyboard: rows}
}

func MainMenuKeyboard() telegram.ReplyKeyboardMarkup {
	return telegram.ReplyKeyboardMarkup{
		Keyboard: [][]telegram.KeyboardButton{
			{{Text: "📋 Мои записи"}, {Text: "💼 Вакансии"}},
			{{Text: "👤 Профиль"}, {Text: "💬 Поддержка"}},
		},
		ResizeKeyboard: true,
	}
}

func ProfileKeyboard() telegram.InlineKeyboardMarkup {
	return telegram.InlineKeyboardMarkup{
		InlineKeyboard: [][]telegram.InlineKeyboardButton{
			{{Text: "✏️ Изменить данные", CallbackData: "workbot:profile_edit"}},
		},
	}
}

func EmptyInlineKeyboard() telegram.InlineKeyboardMarkup {
	return telegram.InlineKeyboardMarkup{InlineKeyboard: [][]telegram.InlineKeyboardButton{}}
}
