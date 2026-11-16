# 13. Словарь данных MarkTrace

**MarkTrace · версия 2.0 · 16.11.2026**

## 1. Назначение

Словарь описывает каждый столбец физической схемы `backend/internal/store/migrations/001_init.sql`, входные CSV, перечисления и коды причин. Диаграмма и связи — в [ERD](12-erd.md). Правила проверок соответствуют `SPEC.md` §1–§3 и пакету `backend/internal/rules`, версия правил `rules_version = 2026.11-1`.

Версия 1.0 от 19.10.2026 была проектным предложением до реализации. Поля и перечисления, которых в коде нет, перечислены в разделе 9 вместе с тем, что их заменило.

Обозначения в таблицах:

- «NULL: нет» — `NOT NULL` в БД;
- «Источник» — откуда берётся значение: CSV, форма, сервер, мок;
- идентификаторы и `actor` сравниваются точно: регистр значим, пробелы не обрезаются ни при вводе, ни при сравнении.

## 2. Типы и общие правила

| Тип в БД | Как используется |
|---|---|
| `text` | Все строковые идентификаторы и тексты. Длина в БД не ограничена; формат проверяют правила (маска кода, GTIN) |
| `timestamptz` | Моменты времени. Вход — RFC 3339 с обязательным смещением, точность до секунды. Хранится как момент UTC, выводится как `2006-01-02T15:04:05Z` |
| `uuid` | Служебные ключи. Генерирует backend (`google/uuid`, v4); из CSV не принимаются |
| `jsonb` | Сырые данные и метаданные, которые читаются целиком: `raw`, `files`, `counters`, `mock_result` |
| `integer` | `line_no`, `rows_created` |

Бизнес-время (`operations.ts`, `codes.ts`, `expected_ts`, `op_ts`) — это время события из входных данных. Системное время (`created_at`, `started_at`, `finished_at`) — момент записи в БД. Историческое событие можно загрузить позже его бизнес-времени: порядок проверяется по истории кода, а не по часам сервера. `created_at` в попытках, нарушениях и разборе задаётся через `clock_timestamp()`, поэтому записи одной транзакции различаются по времени.

## 3. Столбцы таблиц

### 3.1. `users` — учётные записи

| Поле | Тип | NULL | Ключ / ограничение | Источник | Смысл |
|---|---|---|---|---|---|
| `username` | text | нет | PK | seed | Логин: `prod`, `retail`, `qc`, `auditor`, `admin` |
| `password_hash` | text | нет | — | seed | bcrypt-хеш пароля; пароль в открытом виде не хранится |
| `role` | text | нет | CHECK `A1, A2, A3, A4, admin` | seed | Роль, которая определяет права на запись |
| `display_name` | text | нет | — | seed | Подпись в интерфейсе: «Оператор производства», «Оператор розницы», «Контролёр качества», «Аудитор», «Администратор» |

### 3.2. `sessions` — сеансы входа

| Поле | Тип | NULL | Ключ / ограничение | Источник | Смысл |
|---|---|---|---|---|---|
| `token` | text | нет | PK | сервер | 32 случайных байта в hex (64 символа); значение cookie `mt_session` (HttpOnly, SameSite=Lax) |
| `username` | text | нет | FK `users`, `ON DELETE CASCADE` | вход | Владелец сеанса |
| `expires_at` | timestamptz | нет | — | сервер | Время входа плюс 12 ч; после него сеанс недействителен |

### 3.3. `items` — номенклатура

| Поле | Тип | NULL | Ключ / ограничение | Источник | Смысл |
|---|---|---|---|---|---|
| `item_code` | text | нет | PK | `items.csv` | Код товара, например `ITM-0001` |
| `name` | text | нет | — | `items.csv` | Наименование; может содержать запятые и кавычки (RFC 4180) |
| `gtin` | text | нет | — | `items.csv` | GTIN-13 или GTIN-14 в исходном виде, ведущие нули сохраняются. Перед записью проверяется на `GTIN_FORMAT` и `GTIN_CHECKSUM`. Уникальность не требуется |

### 3.4. `code_registry` — привязка кода и ожидаемый снимок

| Поле | Тип | NULL | Ключ / ограничение | Источник | Смысл |
|---|---|---|---|---|---|
| `code` | text | нет | PK | `codes.csv` или форма (`issue`) | Код маркировки по маске `^01(\d{14})21([A-Za-z0-9]{6})$` |
| `item_code` | text | нет | FK `items` | `codes.csv` или форма | Товар кода. Нормализованный GTIN товара совпадает с GTIN внутри кода. После записи не меняется |
| `expected_status` | text | да | — (проверка `CodeStatus` в коде) | `codes.csv` `status` | Ожидаемый итоговый статус. NULL у привязок, созданных формой |
| `expected_ts` | timestamptz | да | — | `codes.csv` `ts` | Ожидаемое время последней операции. NULL у привязок формы |

### 3.5. `codes` — проекция текущего статуса

| Поле | Тип | NULL | Ключ / ограничение | Источник | Смысл |
|---|---|---|---|---|---|
| `code` | text | нет | PK, FK `code_registry` | первый принятый `issue` | Код, у которого есть принятая история |
| `item_code` | text | нет | — (копия `code_registry.item_code`), индекс | реестр | Товар; копия для отчёта |
| `status` | text | нет | CHECK `CodeStatus`, индекс | правило перехода | Статус после последней принятой операции |
| `ts` | timestamptz | нет | — | последняя принятая операция | Её бизнес-время; в API и экспорте называется `last_ts` |
| `last_actor` | text | нет | — | последняя принятая операция | Её `actor`; фильтр «Исполнитель» в отчёте «Коды» |

### 3.6. `operations` — журнал принятых операций

| Поле | Тип | NULL | Ключ / ограничение | Источник | Смысл |
|---|---|---|---|---|---|
| `op_id` | text | нет | PK | CSV или форма (по умолчанию UUID) | Идентификатор операции. Повтор с тем же содержимым — дубль, с другим — `IDEMPOTENCY_CONFLICT` |
| `code` | text | нет | FK `code_registry`; UNIQUE `(code, ts)` | CSV или форма | Код операции |
| `op_type` | text | нет | — (проверка `OperationType` в коде) | CSV или форма | `issue`, `introduce`, `sell`, `return`, `dispose` |
| `ts` | timestamptz | нет | UNIQUE `(code, ts)` | CSV или форма (по умолчанию время сервера) | Бизнес-время; строго больше `ts` предыдущей принятой операции кода |
| `actor` | text | нет | — | CSV или форма (по умолчанию имя пользователя) | Исполнитель из входных данных, например `op-shop-03`. Прав не даёт |
| `attempt_id` | uuid | да | ref `operation_attempts` | сервер | Попытка, которая создала операцию; заполняется всегда, хотя столбец допускает NULL |
| `created_at` | timestamptz | нет | DEFAULT `now()` | сервер | Время записи |

### 3.7. `import_sessions` — сеансы импорта

| Поле | Тип | NULL | Ключ / ограничение | Источник | Смысл |
|---|---|---|---|---|---|
| `session_id` | uuid | нет | PK | сервер | Новый сеанс на каждую загрузку, в том числе повторную |
| `started_by` | text | нет | ref `users` | сеанс | Кто загрузил |
| `role` | text | нет | — | сеанс | Роль загрузившего: A1, A3 или admin |
| `started_at` | timestamptz | нет | DEFAULT `now()` | сервер | Начало сеанса |
| `finished_at` | timestamptz | да | — | сервер | Окончание; NULL, пока статус `running` |
| `status` | text | нет | CHECK `ImportStatus` | сервер | Итог сеанса, см. 4.2 |
| `failure_reason` | text | да | — | сервер | Текст на русском для `structure_rejected` и `failed`, например «Файл items.csv: заголовок «…» не совпадает с ожидаемым «item_code,name,gtin»» |
| `files` | jsonb | нет | DEFAULT `'[]'` | сервер | Массив `{entity, filename, size, sha256}` по загруженным файлам |
| `counters` | jsonb | да | — | сервер | `{"items":{"accepted":96,"duplicate":0,"rejected":3}, …}` по загруженным сущностям |
| `rows_created` | integer | да | — | сервер | Новые строки `items`, `code_registry`, `operations` и `violations` за сеанс. Основа K3 |

### 3.8. `import_rows` — строки CSV

| Поле | Тип | NULL | Ключ / ограничение | Источник | Смысл |
|---|---|---|---|---|---|
| `row_id` | uuid | нет | PK | сервер | Идентификатор строки |
| `session_id` | uuid | нет | FK `import_sessions`, индекс | сервер | Сеанс |
| `entity` | text | нет | CHECK `ImportEntity` | имя поля multipart | Из какого файла строка |
| `line_no` | integer | нет | — | разбор CSV | Номер физической строки файла, на которой начинается запись; заголовок — строка 1 |
| `key` | text | нет | индекс `(entity, key)` | первое поле строки | `item_code`, `code` или `op_id` как в файле; может быть пустой строкой |
| `raw` | jsonb | нет | — | разбор CSV | Все поля строки как JSON-массив строк, без нормализации |
| `processing_status` | text | нет | CHECK `RowStatus` | правила | Итог строки |
| `reason` | text | да | — | правила | Код причины у `rejected`; NULL у `accepted` и `duplicate` |

### 3.9. `operation_attempts` — попытки операций

| Поле | Тип | NULL | Ключ / ограничение | Источник | Смысл |
|---|---|---|---|---|---|
| `attempt_id` | uuid | нет | PK | сервер | Идентификатор попытки; возвращается формой |
| `source` | text | нет | CHECK `form, import` | сервер | Путь поступления |
| `import_row_id` | uuid | да | FK `import_rows` | сервер | Строка CSV у `import`; NULL у `form` |
| `op_id` | text | нет | индекс | CSV или форма | Как введено; у формы без `op_id` — сгенерированный UUID |
| `code` | text | нет | индекс | CSV или форма | Как введено, включая неверный код. FK нет намеренно |
| `op_type` | text | нет | — | CSV или форма | Как введено, включая неизвестный тип (`transfer`) |
| `ts_raw` | text | нет | — | CSV или форма | Время текстом, как введено; у формы без `ts` — время сервера в формате UTC |
| `actor` | text | нет | — | CSV или форма | Как введено; у формы без `actor` — имя пользователя |
| `initiator` | text | нет | ref `users` | сеанс | Кто отправил форму или загрузил файл |
| `role` | text | нет | — | сеанс | Роль инициатора |
| `decision` | text | нет | CHECK `Decision` | правила | Итог попытки |
| `reason` | text | да | — | правила | Код причины у `rejected`; NULL иначе |
| `mock_result` | jsonb | да | — | мок | Тело ответа `/cz/verify` либо синтезированное `{"is_valid":false,"reason":"MOCK_UNAVAILABLE","detail":…}`. Заполняется, только если форма вызывала мок; у импорта всегда NULL |
| `rules_version` | text | нет | — | сервер | `2026.11-1` |
| `created_at` | timestamptz | нет | DEFAULT `clock_timestamp()` | сервер | Время записи; по нему упорядочена лента попыток в карточке кода |

Попытка с ролью, которой операция запрещена (403 `AUTH_DENIED`), не записывается.

### 3.10. `violations` — нарушения

| Поле | Тип | NULL | Ключ / ограничение | Источник | Смысл |
|---|---|---|---|---|---|
| `violation_id` | uuid | нет | PK | сервер | Идентификатор |
| `source` | text | нет | CHECK `items, codes, operations` | сервер | Сущность-источник; формой создаются нарушения с `operations` |
| `ref` | text | нет | — | вход | Ключ источника: `item_code` для items, `code` для codes, `op_id` для operations (как во входе) |
| `code` | text | да | индекс | вход | Код из строки или попытки; NULL у items и при пустом коде |
| `item_code` | text | да | — | вход или реестр | Товар из строки, привязки кода или формы; для фильтра «Товар» |
| `reason` | text | нет | индекс | правила | Один код из раздела 7 |
| `message` | text | нет | — | правила | «Базовый текст: подробность» на русском, например «Недопустимый переход статуса: операция sell из статуса issued (выпущен)» |
| `attempt_id` | uuid | да | ref `operation_attempts` | сервер | Попытка-источник (operations) |
| `import_row_id` | uuid | да | ref `import_rows` | сервер | Строка-источник (импорт) |
| `rules_version` | text | нет | — | сервер | Версия правил на момент проверки |
| `created_at` | timestamptz | нет | DEFAULT `clock_timestamp()`, индекс | сервер | Когда обнаружено; по нему фильтр периода в «Расхождениях» |
| `op_ts` | timestamptz | да | — | вход | Бизнес-время операции, если его удалось разобрать |
| `actor` | text | да | — | вход | Исполнитель операции; для фильтра «Исполнитель» |

### 3.11. `violation_resolutions` — разбор нарушений

| Поле | Тип | NULL | Ключ / ограничение | Источник | Смысл |
|---|---|---|---|---|---|
| `id` | uuid | нет | PK | сервер | Идентификатор действия |
| `violation_id` | uuid | нет | FK `violations`; частичный UNIQUE при `action = 'close'` | запрос | Нарушение |
| `action` | text | нет | CHECK `comment, close` | запрос | Комментарий или закрытие |
| `comment` | text | нет | CHECK `comment <> ''` | запрос | Обоснование; обязательно для обоих действий |
| `author` | text | нет | ref `users` | сеанс | Имя пользователя A3 или admin |
| `created_at` | timestamptz | нет | DEFAULT `clock_timestamp()` | сервер | Время действия |

## 4. Перечисления

### 4.1. `CodeStatus` и `OperationType`

| `OperationType` | Допустим из статуса | `CodeStatus` после приёма |
|---|---|---|
| `issue` | у кода нет принятой истории (нет строки в `codes`) | `issued` |
| `introduce` | `issued` | `introduced` |
| `sell` | `introduced`, `returned` | `sold` |
| `return` | `sold` | `returned` |
| `dispose` | `introduced`, `sold`, `returned` | `disposed` (финальный) |

Порядок статусов в своде и счётчиках: `issued, introduced, sold, returned, disposed`. Значения `accepted`, `rejected`, `duplicate` — итоги попыток и строк, а не статусы кода.

### 4.2. Служебные перечисления

| Перечисление | Значения | Где |
|---|---|---|
| `Role` | `A1`, `A2`, `A3`, `A4`, `admin` | `users.role`, `import_sessions.role`, `operation_attempts.role` |
| `ImportStatus` | `running` (идёт обработка), `completed` (все строки пройдены, ошибки данных возможны), `structure_rejected` (неверный заголовок, кодировка или CSV; строки не обрабатывались), `failed` (техническая ошибка; принятые строки остаются) | `import_sessions.status` |
| `ImportEntity` | `items`, `codes`, `operations` | `import_rows.entity` |
| `RowStatus` | `accepted`, `duplicate`, `rejected` | `import_rows.processing_status` |
| `Source` | `form`, `import` | `operation_attempts.source` |
| `Decision` | `accepted`, `rejected`, `duplicate` | `operation_attempts.decision` |
| `ViolationSource` | `items`, `codes`, `operations` | `violations.source` |
| `ResolutionAction` | `comment`, `close` | `violation_resolutions.action` |
| Состояние нарушения (вычисляется) | `open`, `closed` | API `GET /api/violations`, фильтр `state` |

Для `codes` статус `accepted` значит, что строка принята в реестр. Сам код в `codes` появляется только после принятого `issue`.

## 5. Входные CSV

| Файл | Заголовок (точно, в этом порядке) | Ключ строки | Куда пишет |
|---|---|---|---|
| `items.csv` | `item_code,name,gtin` | `item_code` | `items` |
| `codes.csv` | `code,item_code,status,ts` | `code` | `code_registry` |
| `operations.csv` | `op_id,code,op_type,ts,actor` | `op_id` | `operation_attempts`, `operations`, `codes` |

Формат:

- кодировка UTF-8, начальный BOM отбрасывается;
- разделитель — запятая, кавычки по RFC 4180; голая кавычка внутри поля без кавычек допускается;
- заголовок обязателен и должен совпадать точно. Иначе весь сеанс получает `structure_rejected`, и ни одна строка не обрабатывается;
- строка с неверным числом полей отклоняется с `VALUE_FORMAT`, остальные строки обрабатываются.

Один сеанс может содержать любое непустое подмножество трёх файлов (поля multipart `items`, `codes`, `operations`). Порядок обработки всегда такой: items → codes → operations → сверка снимка. Строки идут в порядке файла, каждая — в своей транзакции. Подробное соответствие полей — в [26-data-mapping.md](26-data-mapping.md).

## 6. Форматы значений

| Значение | Правило | Причина отказа |
|---|---|---|
| GTIN | Только цифры, длина 13 или 14 | `GTIN_FORMAT` |
| GTIN | Контрольная цифра GS1 mod-10: веса 3, 1, 3, … справа налево без контрольной цифры; ожидаемая цифра `(10 − S mod 10) mod 10` | `GTIN_CHECKSUM` |
| GTIN для сравнения | GTIN-13 дополняется ведущим `0` до 14 цифр; нормализованная форма не записывается | — |
| Код маркировки | `^01(\d{14})21([A-Za-z0-9]{6})$`: ровно 24 символа, группа 1 — GTIN-14 внутри кода. Скобочная запись `(01)…(21)…` не проходит | `CODE_FORMAT` |
| Код ↔ товар | Нормализованный GTIN товара равен GTIN внутри кода | `GTIN_MISMATCH` |
| Время | RFC 3339 с `Z` или смещением, ровно секунды: `2026-10-01T09:00:00+03:00`. Без смещения, с долями секунды, со строчными `t`/`z` — отказ | `TIME_FORMAT` |
| Статус в `codes.csv` | Одно из `CodeStatus` | `VALUE_FORMAT` |
| Тип операции | Одно из `OperationType` | `VALUE_FORMAT` |

Контрольные примеры GTIN: `4601234567893` и `04601234567893` верны и при сравнении равны; `4601234567894` даёт `GTIN_CHECKSUM`; `46012345678A3` даёт `GTIN_FORMAT`.

## 7. Коды причин

У каждой отклонённой строки или попытки ровно одна причина: первая неудачная проверка в порядке SPEC §2.3. Сообщение — базовый текст из `rules/reasons.go` плюс подробность.

| Код | Когда | Где возникает |
|---|---|---|
| `AUTH_DENIED` | Роли запрещено действие | Только HTTP 403; в БД не пишется |
| `REQUIRED_FIELD` | Пустое обязательное поле; для `issue` из формы — пустой `item_code` | items, codes, operations, форма |
| `VALUE_FORMAT` | Неверное число полей, неизвестный статус или тип операции | items, codes, operations, форма |
| `CODE_FORMAT` | Код не соответствует маске | codes, operations, форма |
| `GTIN_FORMAT` | GTIN не 13/14 цифр | items |
| `GTIN_CHECKSUM` | Неверная контрольная цифра | items |
| `GTIN_MISMATCH` | GTIN товара не совпадает с GTIN в коде | codes, форма (`issue`) |
| `ITEM_NOT_FOUND` | Товара нет в `items` | codes, форма (`issue`) |
| `CODE_NOT_FOUND` | `issue`: кода нет в `code_registry`; прочие операции: у кода нет принятой истории | operations, форма |
| `DUPLICATE_CODE` | Код уже в реестре с другим содержимым или товаром; повторный `issue` | codes, operations, форма |
| `TRANSITION_NOT_ALLOWED` | Переход не разрешён таблицей 4.1 | operations, форма |
| `TIME_FORMAT` | Время не по формату | codes, operations, форма |
| `TIME_REVERSED` | `ts` не позже последней принятой операции кода | operations, форма |
| `IDEMPOTENCY_CONFLICT` | Тот же `item_code` или `op_id` либо та же пара `(code, ts)`, но содержимое другое | items, operations, форма |
| `SNAPSHOT_MISMATCH` | Итоговые статус или время кода не совпадают с `codes.csv` | сверка снимка (source `codes`) |
| `MOCK_INVALID` | Мок ответил `is_valid=false`; его причина (`INVALID_FORMAT`, `UNKNOWN_CODE`, `GTIN_MISMATCH`) — в тексте сообщения | форма |
| `MOCK_UNAVAILABLE` | Таймаут 2 с, сетевая ошибка, ответ не 200 или без булева `is_valid` | форма, `POST /api/verify` (не сохраняется) |

Повторы, которые не являются нарушениями:

- тот же `item_code` с теми же `name, gtin` → `duplicate`;
- тот же `code` с той же строкой `codes.csv` → `duplicate`;
- тот же `op_id` с теми же `code, op_type`, моментом времени и `actor` → `duplicate`;
- та же пара `(code, ts)` с теми же `op_type, actor` → `duplicate`;
- строка, идентичная ранее отклонённой → `duplicate` без нового нарушения. Исключения: прежний отказ `CODE_NOT_FOUND` или `ITEM_NOT_FOUND`, если код или товар с тех пор появился, и `MOCK_UNAVAILABLE`.

Время сравнивается как момент UTC: `2026-09-01T08:03:33+03:00` и `2026-09-01T05:03:33Z` — одна операция.

## 8. JSON-поля

| Поле | Структура | Пример |
|---|---|---|
| `import_rows.raw` | Массив строк в порядке столбцов файла | `["OP-000001","010460419899417021yOm9sp","issue","2026-09-01T08:03:33+03:00","op-prod-01"]` |
| `import_sessions.files` | Массив объектов | `[{"entity":"items","filename":"items.csv","size":10800,"sha256":"…"}]` |
| `import_sessions.counters` | Объект по сущностям | `{"operations":{"accepted":862,"duplicate":1,"rejected":7}}` |
| `operation_attempts.mock_result` | Ответ мока или синтезированный отказ | `{"is_valid":true}`, `{"is_valid":false,"reason":"UNKNOWN_CODE"}`, `{"is_valid":false,"reason":"MOCK_UNAVAILABLE","detail":"HTTP 500"}` |

## 9. Что изменилось по сравнению с версией 1.0

| Было в 1.0 | Стало |
|---|---|
| Таблица `operation_requests` и поля `source_kind`, `*_raw`, `initiator_role`, `decision_status`, `decided_at`, `operation_id` | `operation_attempts`: `source`, сырые поля без суффикса (кроме `ts_raw`), `initiator` + `role`, `decision`, `created_at`; ссылка на операцию — `operations.attempt_id` |
| Таблица `verification_checks` и `CheckOutcome` | `operation_attempts.mock_result` |
| `RequestDecision`: `received`, `pending`, `failed` | Удалены; `pending` и отложенной перепроверки нет |
| `RequestSource`: `code_check` (UC5) | Удалён; ручная проверка `POST /api/verify` не сохраняется |
| `ImportStatus`: `staging` | Удалён |
| `RowStatus`: `staged`; поля `entity_type`, `record_no`, `raw_record`, `processed_at` | Удалены или переименованы: `entity`, `line_no`, `raw jsonb`, `key`, `reason` |
| `import_sessions.initiator_role`; счётчики не хранятся | `started_by` + `role`; `files`, `counters`, `rows_created` |
| `violations.request_id`, `rule_code`, `reason` (текст), `detected_at` | `attempt_id`, `reason` (код), `message` (текст), `created_at`; добавлены `source`, `ref`, `code`, `item_code`, `op_ts`, `actor`, `rules_version` |
| `violation_resolutions.resolution_id`, `author_role` | `id`, `author` |
| `codes.item_code` — FK на `items` | Привязка в `code_registry`; в `codes` — копия и новое поле `last_actor` |
| `ActorRole` `A1–A4` | `Role` `A1–A4, admin` |
| `RuleCode`: `ITEM_MISMATCH`, `DEPENDENCY_REJECTED`, `TECHNICAL_FAILURE` | Удалены |
| `String(n)`, `PositiveInteger`, `Integer(100..599)` | `text`, `integer`; HTTP-статус мока хранится только в `detail` синтезированного `mock_result` |
| «Маска кода не определена» | Маска и версия правил зафиксированы (раздел 6) |
| «Формат golden не предоставлен» | `golden/violations.csv`: `code,reason,source,ref`; `golden/status_summary.csv`: `status,count` |

## 10. Источники

- [12. ERD](12-erd.md) — связи, ключи, транзакции.
- [24. Нормализация](24-normalization.md), [26. Маппинг данных](26-data-mapping.md).
- `SPEC.md` §1–§4, `README.md` (раздел «Допущения и отличия от логической модели ERD v1.0»).
- `backend/internal/rules` (правила и тексты причин), `backend/internal/importer`, `backend/internal/ops`.
