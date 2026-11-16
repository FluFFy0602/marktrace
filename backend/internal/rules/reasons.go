package rules

// Reason codes (SPEC §1.5), stable and machine-readable.
const (
	AuthDenied           = "AUTH_DENIED"
	RequiredField        = "REQUIRED_FIELD"
	ValueFormat          = "VALUE_FORMAT"
	CodeFormat           = "CODE_FORMAT"
	GTINFormat           = "GTIN_FORMAT"
	GTINChecksum         = "GTIN_CHECKSUM"
	GTINMismatch         = "GTIN_MISMATCH"
	ItemNotFound         = "ITEM_NOT_FOUND"
	CodeNotFound         = "CODE_NOT_FOUND"
	DuplicateCode        = "DUPLICATE_CODE"
	TransitionNotAllowed = "TRANSITION_NOT_ALLOWED"
	TimeFormat           = "TIME_FORMAT"
	TimeReversed         = "TIME_REVERSED"
	IdempotencyConflict  = "IDEMPOTENCY_CONFLICT"
	SnapshotMismatch     = "SNAPSHOT_MISMATCH"
	MockInvalid          = "MOCK_INVALID"
	MockUnavailable      = "MOCK_UNAVAILABLE"
)

// Reasons lists every reason code.
var Reasons = []string{
	AuthDenied, RequiredField, ValueFormat, CodeFormat, GTINFormat, GTINChecksum,
	GTINMismatch, ItemNotFound, CodeNotFound, DuplicateCode, TransitionNotAllowed,
	TimeFormat, TimeReversed, IdempotencyConflict, SnapshotMismatch, MockInvalid,
	MockUnavailable,
}

// messages is the Russian message catalog: one base text per reason code.
var messages = map[string]string{
	AuthDenied:           "Недостаточно прав для этого действия",
	RequiredField:        "Не заполнено обязательное поле",
	ValueFormat:          "Недопустимое значение",
	CodeFormat:           "Код маркировки не соответствует маске 01 + GTIN-14 + 21 + 6 символов",
	GTINFormat:           "GTIN должен состоять из 13 или 14 цифр",
	GTINChecksum:         "Неверная контрольная цифра GTIN",
	GTINMismatch:         "GTIN товара не совпадает с GTIN в коде маркировки",
	ItemNotFound:         "Товар не найден в справочнике",
	CodeNotFound:         "Код маркировки не найден",
	DuplicateCode:        "Код маркировки уже зарегистрирован",
	TransitionNotAllowed: "Недопустимый переход статуса",
	TimeFormat:           "Время должно быть в формате RFC 3339 с часовым поясом, без долей секунды",
	TimeReversed:         "Время операции не позже последней принятой операции по коду",
	IdempotencyConflict:  "Повтор ключа с другим содержимым",
	SnapshotMismatch:     "Итоговое состояние кода не совпадает с ожидаемым",
	MockInvalid:          "Мок-сервис ЦРПТ отклонил код",
	MockUnavailable:      "Мок-сервис ЦРПТ недоступен",
}

// Message returns the Russian message for a reason, optionally with a detail suffix.
func Message(reason, detail string) string {
	base, ok := messages[reason]
	if !ok {
		base = reason
	}
	if detail == "" {
		return base
	}
	return base + ": " + detail
}

// StatusRu returns the Russian name of a code status (for messages).
func StatusRu(s string) string {
	switch s {
	case StatusIssued:
		return "выпущен"
	case StatusIntroduced:
		return "введён в оборот"
	case StatusSold:
		return "продан"
	case StatusReturned:
		return "возвращён"
	case StatusDisposed:
		return "выбыл"
	case "":
		return "нет истории"
	}
	return s
}
