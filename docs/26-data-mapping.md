# 26. Таблица маппинга данных

**MarkTrace · версия 1.0 · 16.11.2026**

Документ закрывает практику 24. Для каждого поля указано, откуда оно приходит, куда записывается или выводится, в каком формате, как преобразуется, как проверяется и какой код причины получает при ошибке. Номера потоков `Fn` и процессов `Pn` — из [DFD](25-dfd.md), таблицы и столбцы — из [ERD](12-erd.md) и [словаря](13-data-dictionary.md). Порядок проверок задаёт `SPEC.md` §2.3: у отклонённой строки ровно одна причина, побеждает первая неудачная проверка.

## 1. Общие правила обмена

| Параметр | Значение |
|---|---|
| Версия контракта | Правила `rules_version = 2026.11-1` (записывается в каждую попытку и каждое нарушение); REST API `/api` без версии в пути; контракт — `SPEC.md` |
| Кодировка CSV | UTF-8, начальный BOM отбрасывается; не UTF-8 → сеанс `structure_rejected` |
| Синтаксис CSV | Запятая, RFC 4180 (`encoding/csv`), голая кавычка внутри поля допускается |
| Заголовок | Точное совпадение имён и порядка, иначе `structure_rejected` с `failure_reason` |
| Число полей в строке | Не совпадает с заголовком → строка `rejected`, `VALUE_FORMAT` |
| Пробелы и регистр | Не меняются ни при вводе, ни при сравнении |
| Время | Вход: RFC 3339 со смещением, без долей секунды. Хранение: `timestamptz`. Выход: UTC `YYYY-MM-DDTHH:MM:SSZ` |
| GTIN | Хранится как введён; для сравнения GTIN-13 → `"0" + gtin13` |
| Триггер | Импорт — пакетно по `POST /api/imports`; форма и проверка — синхронно по запросу; экспорт — по запросу |
| Обработка ошибок | Строка или попытка → `violations` с одной причиной и текстом на русском. HTTP-ошибки API: `{error, message}` с кодом 400, 401, 403, 404, 409, 413 (запрос импорта больше 20 МиБ) или 500 |
| Корреляция | `session_id` (сеанс импорта), `row_id` и `line_no` (строка), `attempt_id` (попытка), `violation_id`. Заголовка `X-Request-Id` нет |
| Персональные данные | Нет. `actor` — синтетический идентификатор (`op-shop-03`). `users.username/display_name` — демо-учётные записи. Пароль хранится только как bcrypt-хеш |

## 2. `items.csv` → `items` (P3 → P4, F19, F23, F25)

| Поле source | Поле target | Тип | Обяз. | Трансформация | Валидация → причина |
|---|---|---|---|---|---|
| строка целиком | `import_rows.raw` | jsonb (массив строк) | да | без изменений | 3 поля, иначе `VALUE_FORMAT` |
| номер строки файла | `import_rows.line_no` | integer | да | строка начала записи, заголовок = 1 | — |
| `item_code` | `items.item_code`, `import_rows.key`, `violations.ref`, `violations.item_code` | text | да | без изменений | пусто → `REQUIRED_FIELD`; уже есть с другими `name/gtin` → `IDEMPOTENCY_CONFLICT`; те же значения → `duplicate` |
| `name` | `items.name` | text | да | без изменений; кавычки и запятые по RFC 4180 | пусто → `REQUIRED_FIELD` |
| `gtin` | `items.gtin` | text (13/14 цифр) | да | без изменений, ведущий ноль сохраняется | не 13/14 цифр → `GTIN_FORMAT`; контрольная цифра → `GTIN_CHECKSUM` |

Итог строки → `import_rows.processing_status`, `import_rows.reason`. Счётчик → `import_sessions.counters.items`.

## 3. `codes.csv` → `code_registry` (P3 → P4, F19, F23, F27)

| Поле source | Поле target | Тип | Обяз. | Трансформация | Валидация → причина |
|---|---|---|---|---|---|
| `code` | `code_registry.code`, `import_rows.key`, `violations.ref`, `violations.code` | text (24 символа) | да | без изменений | пусто → `REQUIRED_FIELD`; уже в реестре с другим содержимым → `DUPLICATE_CODE`; маска `^01(\d{14})21([A-Za-z0-9]{6})$` → `CODE_FORMAT` |
| `item_code` | `code_registry.item_code`, `violations.item_code` | text | да | без изменений | нет в `items` → `ITEM_NOT_FOUND`; нормализованный GTIN товара ≠ группе 1 маски → `GTIN_MISMATCH` |
| `status` | `code_registry.expected_status` | text, `CodeStatus` | да | без изменений | не из 5 статусов → `VALUE_FORMAT` |
| `ts` | `code_registry.expected_ts` | timestamptz | да | RFC 3339 → момент UTC | формат → `TIME_FORMAT` |

Порядок проверок: `REQUIRED_FIELD` → повтор/`DUPLICATE_CODE` → `CODE_FORMAT` → `VALUE_FORMAT` → `TIME_FORMAT` → `ITEM_NOT_FOUND` → `GTIN_MISMATCH`.

**Сверка снимка.** После строк сеанса, если в нём был `codes.csv` или `operations.csv`, каждая строка `code_registry` с непустым ожиданием сравнивается с `codes`:

- `expected_status` ↔ `codes.status`;
- `expected_ts` ↔ `codes.ts` (как моменты).

Расхождение → `violations` с `source=codes`, `ref=code`, `reason=SNAPSHOT_MISMATCH` и `message` вида «ожидалось статус …, ts …; фактически …». Если такое же нарушение уже есть, открытое или закрытое, новое не создаётся.

## 4. `operations.csv` → `operation_attempts`, `operations`, `codes` (P3 → P4, F23, F29, F31, F33)

| Поле source | Поле target | Тип | Обяз. | Трансформация | Валидация → причина |
|---|---|---|---|---|---|
| `op_id` | `operation_attempts.op_id`; при приёме `operations.op_id`; `violations.ref` | text | да | без изменений | пусто → `REQUIRED_FIELD`; принят с другим содержимым → `IDEMPOTENCY_CONFLICT`; то же содержимое → `duplicate` |
| `code` | `operation_attempts.code`; `operations.code`; `codes.code`; `violations.code` | text | да | без изменений | `CODE_FORMAT`; `issue` — нет в реестре, прочие — нет истории → `CODE_NOT_FOUND`; `issue` по коду с историей → `DUPLICATE_CODE` |
| `op_type` | `operation_attempts.op_type`; `operations.op_type` | text, `OperationType` | да | без изменений | не из 5 типов → `VALUE_FORMAT`; переход не по таблице → `TRANSITION_NOT_ALLOWED` |
| `ts` | `operation_attempts.ts_raw` (текст); `operations.ts`, `codes.ts`, `violations.op_ts` | text → timestamptz | да | RFC 3339 → момент UTC | формат → `TIME_FORMAT`; `(code, ts)` занята другой операцией → `IDEMPOTENCY_CONFLICT`, той же → `duplicate`; не позже последней принятой → `TIME_REVERSED` |
| `actor` | `operation_attempts.actor`; `operations.actor`; `codes.last_actor`; `violations.actor` | text | да | без изменений | пусто → `REQUIRED_FIELD` |
| — (сеанс) | `operation_attempts.initiator`, `role` | text | да | из cookie-сеанса | роль импорта: A1, A3, admin, иначе 403 |
| — (вычисляется) | `codes.status` | text | да | `NextStatus(op_type, текущий статус)` | — |
| — (реестр) | `codes.item_code` | text | да | копия `code_registry.item_code` | — |
| — (сервер) | `operation_attempts.source = import`, `import_row_id`, `decision`, `reason`, `rules_version` | — | да | — | — |

Порядок проверок: 1 `REQUIRED_FIELD`, 2 повтор `op_id`, 3 `op_type`, 4 `ts`, 5 маска, 6 `(code, ts)`, 7 существование, 8 повторный `issue`, 9 переход, 10 время. При приёме в одной транзакции пишутся `operations` и upsert `codes`. Мок при импорте не вызывается. Строка, идентичная ранее отклонённой, получает `duplicate` без нового нарушения. Исключения — прежние отказы `CODE_NOT_FOUND` (если код с тех пор появился) и `MOCK_UNAVAILABLE`.

## 5. Служебные поля импорта → `import_sessions`, `import_rows` (P3, F21, F22)

| Источник | Поле target | Тип | Правило |
|---|---|---|---|
| поле multipart `items`/`codes`/`operations` | `import_rows.entity`; `files[].entity` | text | имя поля = сущность; хотя бы один файл обязателен |
| имя файла, размер, содержимое | `import_sessions.files[] = {entity, filename, size, sha256}` | jsonb | SHA-256 содержимого; по совпадению всех трёх хешей K3 распознаёт повторный импорт |
| пользователь сеанса | `import_sessions.started_by`, `role` | text | из cookie |
| итог каждой строки | `import_sessions.counters.<entity>.{accepted, duplicate, rejected}` | jsonb | сумма по строкам сущности |
| новые строки | `import_sessions.rows_created` | integer | +1 за принятую строку (`items`, `code_registry`, `operations`), +1 за нарушение отклонённой строки, + число новых `SNAPSHOT_MISMATCH`; дубли — 0 |
| ошибка заголовка, кодировки, CSV | `import_sessions.status = structure_rejected`, `failure_reason` | text | строки не обрабатываются |
| техническая ошибка | `status = failed`, `failure_reason` | text | уже принятые строки остаются |

## 6. Форма операции `POST /api/operations` (P2 → P4, F11, F12, F17, F18)

| Поле запроса | Поле target | Тип | Обяз. | По умолчанию / трансформация | Валидация → причина |
|---|---|---|---|---|---|
| — (сеанс) | `operation_attempts.initiator`, `role` | text | да | из cookie | `issue`/`introduce` — A1, `sell`/`return`/`dispose` — A2, admin — все; иначе 403 `AUTH_DENIED`, попытка не пишется |
| `op_type` | как в разделе 4 | text | да | — | `REQUIRED_FIELD`, `VALUE_FORMAT`, `TRANSITION_NOT_ALLOWED` |
| `code` | как в разделе 4 | text | да | — | `REQUIRED_FIELD`, `CODE_FORMAT`, `CODE_NOT_FOUND`, `DUPLICATE_CODE` |
| `item_code` | `code_registry.item_code` (новая привязка при `issue`) | text | для `issue` | привязка пишется с `expected_status = expected_ts = NULL` | пусто → `REQUIRED_FIELD`; код привязан к другому товару → `DUPLICATE_CODE`; нет товара → `ITEM_NOT_FOUND`; GTIN → `GTIN_MISMATCH` |
| `ts` | `ts_raw`, `operations.ts` | text → timestamptz | нет | время сервера, UTC до секунды | `TIME_FORMAT`, `TIME_REVERSED`, `IDEMPOTENCY_CONFLICT` |
| `actor` | `actor`, `last_actor` | text | нет | имя пользователя | — |
| `op_id` | `op_id` | text | нет | UUID v4 | `IDEMPOTENCY_CONFLICT`, `duplicate` |
| ответ мока | `operation_attempts.mock_result` | jsonb | — | тело ответа как есть | раздел 7 |

Ответ формы (F12): `{attempt_id, op_id, decision, reason?, message?, code?}`. Объект `code` строится из `codes`: `{code, item_code, status, last_ts (UTC), last_actor}`. При дубле `message` = «Повтор: операция уже учтена, изменений нет».

## 7. Мок ЦРПТ `/cz/verify` (P2 ⇄ E2, F15, F16)

### 7.1. Запрос backend → мок

| Источник | Поле запроса | Тип | Обяз. | Правило |
|---|---|---|---|---|
| форма `code` | `code` | string | да | без изменений |
| `items.gtin` товара кода (из привязки или `item_code` формы) | `gtin` | string | нет | передаётся как хранится (13 или 14 цифр); нормализует мок; не передаётся, если товар неизвестен |

Мок вызывается только после того, как попытка прошла все проверки 1–10. Таймаут — 2 с.

### 7.2. Ответ мок → backend

| Поле ответа | Назначение | Правило |
|---|---|---|
| тело целиком | `operation_attempts.mock_result` | сохраняется без изменений |
| `is_valid = true` | решение остаётся `accepted` | далее перепроверка под блокировкой и запись |
| `is_valid = false`, `reason` | `decision = rejected`, `reason = MOCK_INVALID`, `message` «Мок-сервис ЦРПТ отклонил код: причина: <reason>» | `reason` мока: `INVALID_FORMAT`, `UNKNOWN_CODE`, `GTIN_MISMATCH` |
| таймаут, сетевая ошибка, HTTP ≠ 200, нет булева `is_valid` | `reason = MOCK_UNAVAILABLE`; `mock_result = {"is_valid":false,"reason":"MOCK_UNAVAILABLE","detail":…}` | повтор с тем же `op_id` проверяется заново |

Порядок проверок в моке: маска → `INVALID_FORMAT`; кода нет в `mock_registry.csv` → `UNKNOWN_CODE`; `gtin` задан и нормализованный ≠ GTIN в коде → `GTIN_MISMATCH`; иначе `{is_valid:true}`. Режимы `normal`, `slow` (5 с), `error` (HTTP 500), `garbage` (`{}`) переключаются через `POST /admin/mode`.

`POST /api/verify {code, gtin?}` (страница «Проверка кода», F13/F14) передаёт поля в мок без изменений и возвращает его JSON. Если мок недоступен — `{is_valid:false, reason:"MOCK_UNAVAILABLE", message}`. В БД ничего не пишется.

## 8. Выгрузки (P5 → E1*, U*, F52, F54)

### 8.1. Экспорт кодов `GET /api/codes/export.csv`

| Источник | Столбец CSV | Формат | Правило |
|---|---|---|---|
| `codes.code` | `code` | text | сортировка по байтам (`COLLATE "C"`) |
| `codes.item_code` | `item_code` | text | — |
| `codes.status` | `status` | `CodeStatus` | — |
| `codes.ts` | `last_ts` | `YYYY-MM-DDTHH:MM:SSZ` | перевод в UTC |

Фильтры `status`, `item_code`, `actor` (= `last_actor`), `from`/`to` (по `last_ts`; дата без времени для `to` включает весь день), `q` (подстрока кода) те же, что в `GET /api/codes`. JSON-строки отчёта содержат ещё `last_actor`.

### 8.2. Свод статусов `GET /api/status-summary.csv`

| Источник | Столбец | Правило |
|---|---|---|
| значение `CodeStatus` | `status` | все 5 в порядке `issued, introduced, sold, returned, disposed`, нули включены |
| `count(*)` из `codes` по статусу | `count` | без фильтров |

### 8.3. Нарушения `GET /api/violations/export.csv`

| Источник | Столбец | Правило |
|---|---|---|
| `violations.code` | `code` | NULL → пустая строка (нарушения `items`) |
| `violations.reason` | `reason` | код причины |
| `violations.source` | `source` | `items`, `codes`, `operations` |
| `violations.ref` | `ref` | `item_code` / `code` / `op_id` как во входе |

Сортировка: `source` (items < codes < operations), затем `ref`, `reason`, `code` по байтам. Фильтры те же, что в `GET /api/violations`, включая `state=open|closed`. Значения выводятся как во входе, защиты от формул Excel нет (`README.md`).

### 8.4. Сверка с эталоном `GET /api/golden/compare` (F55)

Текущие 8.2 и 8.3 без фильтров сравниваются с `golden/status_summary.csv` и `golden/violations.csv` как мультимножества строк. Ответ: `{status_summary: {match, expected_rows, actual_rows, missing, extra}, violations: {…}, percent}`. `percent` = совпавшие строки / (ожидаемые строки + лишние строки) × 100 — это K5.

## 9. Пример одной сущности: операция `OP-000001`

Путь одной строки T1 через систему:

| Шаг | Где | Значение |
|---|---|---|
| 1. Справочник | `items.csv`, строка 11 → `items` | `ITM-0010`, «Масло сливочное 82,5%, 180 г, ТМ «Северная ферма»», `04604198994170` |
| 2. Привязка | `codes.csv`, строка 2 → `code_registry` | `010460419899417021yOm9sp`, `ITM-0010`, ожидание `issued`, `2026-09-01T05:03:33Z`. Группа 1 маски `04604198994170` = GTIN товара |
| 3. Строка операции | `operations.csv`, строка 2 → `import_rows` | `line_no=2`, `key=OP-000001`, `raw=["OP-000001","010460419899417021yOm9sp","issue","2026-09-01T08:03:33+03:00","op-prod-01"]` |
| 4. Попытка | `operation_attempts` | `source=import`, `ts_raw="2026-09-01T08:03:33+03:00"`, `initiator=qc`, `role=A3`, `decision=accepted`, `mock_result=NULL` |
| 5. Журнал | `operations` | `OP-000001`, код, `issue`, `ts = 2026-09-01T05:03:33Z`, `actor=op-prod-01`, `attempt_id` из шага 4 |
| 6. Проекция | `codes` | `status=issued`, `ts = 2026-09-01T05:03:33Z`, `last_actor=op-prod-01`, `item_code=ITM-0010` |
| 7. Сверка снимка | `code_registry` ↔ `codes` | `issued` = `issued`, время совпало — нарушения нет |
| 8. Выгрузка | `GET /api/codes/export.csv` | `010460419899417021yOm9sp,ITM-0010,issued,2026-09-01T05:03:33Z` |
| 9. Повторный импорт | шаг 2 правил | те же `code, op_type`, момент `ts`, `actor` → `duplicate`; новых строк в `operations` нет |

## 10. Тестовый набор T1 и эталоны

Набор строит генератор `tools/gen` (`go run . -seed 16`, детерминированно). Генератор не использует код backend: эталон получен из того, что генератор внедрил, а не из прогона правил.

| Файл | Строк данных | Состав |
|---|---|---|
| `data/C16/T1/items.csv` | 99 | 96 корректных (19 с GTIN-13) + 2 `GTIN_CHECKSUM` + 1 `GTIN_FORMAT` |
| `data/C16/T1/codes.csv` | 518 | 512 корректных (из них 2 с намеренно неверным ожиданием → `SNAPSHOT_MISMATCH`) + 2 `CODE_FORMAT` + 2 `DUPLICATE_CODE` + 1 `GTIN_MISMATCH` + 1 `ITEM_NOT_FOUND` |
| `data/C16/T1/operations.csv` | 870 | 862 корректных + 3 `TRANSITION_NOT_ALLOWED` + `TIME_REVERSED` + `CODE_NOT_FOUND` + `VALUE_FORMAT` + `IDEMPOTENCY_CONFLICT` + 1 точный дубль |
| `data/C16/mock_registry.csv` | 542 | Коды, известные моку: 512 из T1 + 30 демо |
| `data/C16/demo_codes.csv` | 30 | `code,item_code` — свободные коды для демонстрации D16 через форму |
| `golden/status_summary.csv` | 5 | issued 300, introduced 110, sold 78, returned 12, disposed 12 |
| `golden/violations.csv` | 18 | 3 items + 8 codes (6 строк + 2 снимка) + 7 operations |

Размеры укладываются в требования кейса C16 (items 80–120, codes 500–800, operations 600–900) и в требование КТ3 «не менее ста строк по ключевой таблице».

Ожидаемый результат на чистой БД после одного импорта трёх файлов:

| Показатель | Значение |
|---|---|
| items: accepted / rejected | 96 / 3 |
| codes: accepted / rejected | 512 / 6 |
| operations: accepted / duplicate / rejected | 862 / 1 / 7 |
| Нарушений | 18 |
| `rows_created` | 1488 = 99 (items) + 518 (codes) + 869 (operations, без дубля) + 2 (снимок) |
| K1 | 862 / (862 + 7) = 99,19% |
| K2 / K5 | 100% / 100% |
| Повторный импорт тех же файлов | все строки `duplicate`, `rows_created = 0` (K3) |

Как проверить:

- `make verify` — чистый стек, импорт T1, `/api/golden/compare` = 100%, K1 ≥ 99, K2 = 100, повторный импорт с `rows_created = 0`, 403 для аудитора;
- автотесты: `backend/internal/importer/importer_test.go`, `backend/internal/httpapi/golden_test.go`, независимый чекер `tools/gen/check`.

Полная таблица внедрённых ошибок с номерами строк — в `tools/gen/README.md`.
