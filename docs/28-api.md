# 28. Спецификация API и отчёт по интеграции

**MarkTrace · версия 1.0 · 16.11.2026**

## 1. Назначение и основание

Документ описывает все точки интеграции MVP: REST API backend (`/api/*`), контракт мок-сервиса ЦРПТ (`/cz/verify`) и обмен файлами CSV. Это артефакты практик 25 «Описание точек интеграции (API)» и 26 «Логика взаимодействия и отчёт». Машиночитаемая версия — [openapi.yaml](openapi.yaml) (OpenAPI 3.0.3). Архитектура и последовательности вызовов — [27-architecture.md](27-architecture.md); потоки данных — [dfd-1.png](diagrams/dfd-1.png); контракт реализации — [SPEC.md](../SPEC.md) §6, §8.

Примеры ответов с пометкой «стенд» сняты 16.11.2026 с запущенного стенда (`make verify`: чистая БД, импорт T1, повторный импорт). Ответы на `POST /api/operations` и `POST /api/violations/{id}/resolutions` с этого стенда не снимались, чтобы не менять данные: они собраны по коду (`ops.FormResponse`, `reports.Resolution`) и помечены «по контракту»; идентификаторы в них условные.

## 2. Общие правила

| Параметр | Значение |
|---|---|
| Базовый URL | `http://localhost:8080/api` — через nginx контейнера `web` (как работает SPA); `http://localhost:8081/api` — напрямую в backend |
| Формат | JSON UTF-8 (`Content-Type: application/json; charset=utf-8`); выгрузки — `text/csv; charset=utf-8` с `Content-Disposition: attachment` |
| Аутентификация | `POST /api/auth/login` ставит cookie `mt_session` (HttpOnly, SameSite=Lax, Path=/, 12 ч). Без действующего сеанса любой эндпойнт, кроме `login`, `logout` и `healthz`, отвечает 401 |
| Авторизация | Роль пользователя проверяется на каждом запросе по таблице раздела 3 |
| Время | Вход — RFC 3339 с часовым поясом, до секунды (`2026-10-01T09:00:00+03:00`, `…Z`); выход — всегда UTC `2006-01-02T15:04:05Z` |
| Идентификаторы | Код маркировки — 24 символа `01` + GTIN-14 + `21` + 6 символов; GTIN — 13 или 14 цифр строкой; служебные id — UUID |
| Пустые значения | Необязательные поля отдаются как `null` (`reason`, `finished_at` и т. п.); пустые списки — `[]` |
| Версия | Префикса версии в пути нет; версия API — 1.0 (`info.version` в OpenAPI). Версия правил `rules_version = 2026.11-1` отдаётся в `GET /api/rules` и пишется в каждую попытку и расхождение |
| Лимиты | JSON-тело до 1 МБ; загрузка импорта до 20 МБ (nginx пропускает до 50 МБ); `limit` в списках — до 5000 |
| Неизвестный путь или метод | 404 `{"error":"404","message":"Ресурс не найден"}` (405 не используется) |

## 3. Перечень эндпойнтов и роли

Роли: A1 — оператор производства (`prod`), A2 — оператор розницы (`retail`), A3 — контролёр качества (`qc`), A4 — аудитор (`auditor`), admin (`admin`). Пароль демо-пользователя совпадает с логином.

| № | Метод и путь | A1 | A2 | A3 | A4 | admin | Назначение | UC |
|---|---|---|---|---|---|---|---|---|
| 1 | `POST /auth/login` | + | + | + | + | + | Вход, установка cookie | — |
| 2 | `POST /auth/logout` | + | + | + | + | + | Выход | — |
| 3 | `GET /auth/me` | + | + | + | + | + | Текущий пользователь | — |
| 4 | `GET /rules` | + | + | + | + | + | Версия правил, маска, переходы, причины | — |
| 5 | `GET /items` | + | + | + | + | + | Справочник товаров | UC1 |
| 6 | `POST /operations` | issue, introduce | sell, return, dispose | — | — | все | Операция через форму | UC1–UC4 |
| 7 | `GET /codes` | + | + | + | + | + | Отчёт «Статусы» | UC6 |
| 8 | `GET /codes/export.csv` | + | + | + | + | + | Выгрузка статусов | UC6 |
| 9 | `GET /codes/{code}` | + | + | + | + | + | История кода | UC5, UC6 |
| 10 | `GET /status-summary.csv` | + | + | + | + | + | Сводка по статусам (формат golden) | UC6 |
| 11 | `GET /violations` | + | + | + | + | + | Журнал расхождений | UC5 |
| 12 | `GET /violations/export.csv` | + | + | + | + | + | Выгрузка расхождений (формат golden) | UC5 |
| 13 | `POST /violations/{id}/resolutions` | — | — | + | — | + | Комментарий или закрытие | UC5 |
| 14 | `GET /kpi` | + | + | + | + | + | K1–K5 | — |
| 15 | `POST /imports` | + | — | + | — | + | Импорт CSV | UC7 |
| 16 | `GET /imports` | + | + | + | + | + | Список сессий импорта | UC7 |
| 17 | `GET /imports/{id}` | + | + | + | + | + | Сессия и отклонённые строки | UC7 |
| 18 | `GET /golden/compare` | + | + | + | + | + | Сверка с эталоном (K5) | — |
| 19 | `POST /verify` | + | + | + | + | + | Ручная проверка кода в моке | UC1–UC5 (шаг) |
| 20 | `GET /healthz` | без входа | | | | | Живость backend | — |

Пути указаны относительно `/api`. `POST /verify` не меняет данные, поэтому открыт и аудитору.

## 4. Формат ошибок и коды ответа

Ошибка HTTP-уровня всегда имеет одно тело:

```json
{"error": "<КОД_ПРИЧИНЫ или HTTP-код строкой>", "message": "<текст на русском>", "reason": "AUTH_DENIED"}
```

Поле `reason` присутствует только у 403 `AUTH_DENIED`. Бизнес-отказ операции — не ошибка HTTP: `POST /operations` отвечает 200 с `decision: "rejected"` и машинной причиной в `reason`.

| Код | Когда | `error` | Пример `message` (стенд) |
|---|---|---|---|
| 200 | Успех; в том числе отклонённая или повторная операция, завершённый или отклонённый по структуре импорт | — | — |
| 201 | Создан комментарий или закрытие расхождения | — | — |
| 400 | Некорректный JSON | `400` | «Некорректный JSON в запросе» |
| 400 | Неверный фильтр (`from`, `to`, `state`, `limit`, `offset`) | `VALUE_FORMAT` | «Недопустимое значение: bad filter: дата «yesterday» не распознана» |
| 400 | Импорт без файлов / не multipart | `REQUIRED_FIELD` / `400` | «Не заполнено обязательное поле: нужен хотя бы один файл: items, codes или operations» |
| 400 | Разбор: `action` не `comment`/`close`; пустой `comment` | `VALUE_FORMAT` / `REQUIRED_FIELD` | «Не заполнено обязательное поле: comment» |
| 401 | Нет сеанса или он истёк; неверный пароль | `401` | «Требуется вход в систему»; «Неверный логин или пароль» |
| 403 | Роль не допускает действие | `AUTH_DENIED` | «Недостаточно прав для этого действия: роль A1 не может выполнять операцию sell» |
| 404 | Код неизвестен системе | `CODE_NOT_FOUND` | «Код маркировки не найден: 0100000000000000210000AA» |
| 404 | Нет сессии импорта / расхождения / пути | `404` | «Сессия импорта не найдена»; «Расхождение не найдено»; «Ресурс не найден» |
| 409 | Повторное закрытие расхождения | `409` | «Расхождение уже закрыто» |
| 413 | Загрузка больше 20 МБ | `413` | «Размер загрузки превышает 20 МБ» |
| 500 | Внутренняя ошибка (детали только в журнале сервера) | `500` | «Внутренняя ошибка сервера» |

Коды причин (17, стабильные): `AUTH_DENIED, REQUIRED_FIELD, VALUE_FORMAT, CODE_FORMAT, GTIN_FORMAT, GTIN_CHECKSUM, GTIN_MISMATCH, ITEM_NOT_FOUND, CODE_NOT_FOUND, DUPLICATE_CODE, TRANSITION_NOT_ALLOWED, TIME_FORMAT, TIME_REVERSED, IDEMPOTENCY_CONFLICT, SNAPSHOT_MISMATCH, MOCK_INVALID, MOCK_UNAVAILABLE`. У каждой отклонённой строки или попытки ровно одна причина — первая непройденная проверка в порядке SPEC §2.3.

## 5. Описание эндпойнтов

### 5.1. Аутентификация

**`POST /api/auth/login`** — тело `{username, password}` (оба обязательны).

```http
POST /api/auth/login
Content-Type: application/json

{"username":"auditor","password":"auditor"}
```

Ответ 200 (стенд):

```http
HTTP/1.1 200 OK
Set-Cookie: mt_session=85fd8fe6…bb60c; Path=/; Max-Age=43200; HttpOnly; SameSite=Lax

{"username":"auditor","role":"A4","display_name":"Аудитор"}
```

Ошибки: 400 — некорректный JSON; 401 — `{"error":"401","message":"Неверный логин или пароль"}` (стенд). Каждый вход создаёт новый сеанс; просроченные сеансы удаляются при входе.

**`POST /api/auth/logout`** — удаляет сеанс из cookie (если он есть) и стирает cookie. Ответ 200 `{"ok":true}`. Сеанс не обязателен.

**`GET /api/auth/me`** — 200 `{"username":"auditor","role":"A4","display_name":"Аудитор"}` (стенд); 401 без сеанса.

### 5.2. Справочники

**`GET /api/rules`** — 200 (стенд):

```json
{"code_mask":"^01(\\d{14})21([A-Za-z0-9]{6})$",
 "op_types":["issue","introduce","sell","return","dispose"],
 "reasons":["AUTH_DENIED","REQUIRED_FIELD","VALUE_FORMAT","…","MOCK_UNAVAILABLE"],
 "rules_version":"2026.11-1",
 "statuses":["issued","introduced","sold","returned","disposed"],
 "transitions":[{"op_type":"issue","from":[],"to":"issued"},
                {"op_type":"introduce","from":["issued"],"to":"introduced"},
                {"op_type":"sell","from":["introduced","returned"],"to":"sold"},
                {"op_type":"return","from":["sold"],"to":"returned"},
                {"op_type":"dispose","from":["introduced","sold","returned"],"to":"disposed"}]}
```

**`GET /api/items`** — массив `{item_code, name, gtin}`, на стенде 96 товаров. Пример элемента: `{"item_code":"ITM-0069","name":"Вода питьевая детская, 5 л, ТМ «Родник Алтая»","gtin":"04689135926483"}`.

### 5.3. Операция через форму

**`POST /api/operations`** — роли A1, A2, admin (дополнительно по типу операции, раздел 3).

| Поле | Тип | Обязательно | По умолчанию / правило |
|---|---|---|---|
| `op_type` | `issue / introduce / sell / return / dispose` | да | иное значение → `VALUE_FORMAT` |
| `code` | строка 24 символа | да | маска → `CODE_FORMAT` |
| `item_code` | строка | только для `issue` | товар должен существовать (`ITEM_NOT_FOUND`), его GTIN — совпасть с GTIN в коде (`GTIN_MISMATCH`) |
| `ts` | RFC 3339 с поясом | нет | текущее время сервера (UTC, секунды) |
| `actor` | строка | нет | логин пользователя |
| `op_id` | строка | нет | новый UUID; передайте свой для безопасного повтора |

Ответ 200 — `{attempt_id, op_id, decision, reason?, message?, code?}`; `code` — текущая проекция кода после операции, отсутствует, если у кода нет истории.

Принято (по контракту):

```json
// запрос пользователя prod (A1)
{"op_type":"issue","code":"010468913592648321BKVhBt","item_code":"ITM-0069","ts":"2026-11-16T10:00:00+03:00"}
// ответ
{"attempt_id":"9a0c…","op_id":"5d1e…","decision":"accepted",
 "code":{"code":"010468913592648321BKVhBt","item_code":"ITM-0069","status":"issued",
         "last_ts":"2026-11-16T07:00:00Z","last_actor":"prod"}}
```

Отклонено правилом (по контракту; `retail` пытается продать выпущенный, но не введённый код):

```json
{"attempt_id":"…","op_id":"…","decision":"rejected","reason":"TRANSITION_NOT_ALLOWED",
 "message":"Недопустимый переход статуса: операция sell из статуса issued (выпущен)",
 "code":{"code":"010468913592648321BKVhBt","item_code":"ITM-0069","status":"issued",
         "last_ts":"2026-11-16T07:00:00Z","last_actor":"prod"}}
```

Отклонено моком: `"reason":"MOCK_INVALID","message":"Мок-сервис ЦРПТ отклонил код: причина: UNKNOWN_CODE"`; мок недоступен: `"reason":"MOCK_UNAVAILABLE","message":"Мок-сервис ЦРПТ недоступен"`. Повтор: `"decision":"duplicate","message":"Повтор: операция уже учтена, изменений нет"`.

Роль не подходит к типу операции — 403, попытка не записывается:

```json
{"error":"AUTH_DENIED","message":"Недостаточно прав для этого действия: роль A1 не может выполнять операцию sell","reason":"AUTH_DENIED"}
```

### 5.4. Отчёт «Статусы» и история кода

**`GET /api/codes`** — параметры (все необязательные):

| Параметр | Смысл |
|---|---|
| `status` | Один из пяти статусов |
| `item_code` | Точное совпадение |
| `actor` | Последний исполнитель (`last_actor`), точное совпадение |
| `from`, `to` | Период по `last_ts`: RFC 3339, `YYYY-MM-DDTHH:MM[:SS]` или `YYYY-MM-DD HH:MM[:SS]` (UTC), `YYYY-MM-DD` (для `to` включает весь день) |
| `q` | Подстрока кода |
| `limit`, `offset` | По умолчанию 100 и 0; `limit` больше 5000 урезается до 5000 |

Сортировка — по коду (побайтно). `counters` учитывают все фильтры, кроме `status`; `total` — число строк с учётом всех фильтров; `elapsed_ms` — длительность запроса на сервере (K4). Ответ 200 (стенд, `?status=returned&limit=1`):

```json
{"rows":[{"code":"010460149653197721Znm0PU","item_code":"ITM-0040","status":"returned",
          "last_ts":"2026-10-16T07:52:21Z","last_actor":"op-shop-02"}],
 "total":12,
 "counters":{"disposed":12,"introduced":110,"issued":300,"returned":12,"sold":78},
 "elapsed_ms":0.47}
```

**`GET /api/codes/export.csv`** — те же фильтры, без пагинации; заголовок `code,item_code,status,last_ts`, сортировка по коду. Стенд (`?status=disposed`):

```csv
code,item_code,status,last_ts
010460419886365021BmcTi1,ITM-0018,disposed,2026-09-21T14:30:22Z
010460419886365021xqnS6z,ITM-0018,disposed,2026-09-30T15:19:29Z
```

**`GET /api/status-summary.csv`** — все пять статусов в порядке жизненного цикла, нули включены. Стенд:

```csv
status,count
issued,300
introduced,110
sold,78
returned,12
disposed,12
```

**`GET /api/codes/{code}`** — `{code, current, registry, item, operations[], attempts[], violations[]}`: проекция, привязка с ожидаемым снимком, товар, принятые операции (по `ts`), все попытки (по времени создания) и расхождения кода. 404 `CODE_NOT_FOUND`, если о коде нет ничего. Стенд (сокращено, код с отклонённой продажей «в прошлом»):

```json
{"code":"010460640976874721fR8UzN",
 "current":{"code":"010460640976874721fR8UzN","item_code":"ITM-0039","status":"introduced",
            "last_ts":"2026-09-09T09:07:45Z","last_actor":"op-prod-02"},
 "registry":{"code":"010460640976874721fR8UzN","item_code":"ITM-0039",
             "expected_status":"introduced","expected_ts":"2026-09-09T09:07:45Z"},
 "item":{"item_code":"ITM-0039","name":"Сандалии детские, р. 28, ТМ «Ступень»","gtin":"04606409768747"},
 "operations":[
   {"op_id":"OP-000113","code":"010460640976874721fR8UzN","op_type":"issue","ts":"2026-09-08T11:36:19Z",
    "actor":"op-prod-02","attempt_id":"1e5f1289-8bd0-4df0-9477-75bc33a898d4","created_at":"2026-11-16T15:51:24Z"},
   {"op_id":"OP-000133","op_type":"introduce","ts":"2026-09-09T09:07:45Z","…":"…"}],
 "attempts":[
   {"attempt_id":"1a6ef1bb-3f0c-40cf-b720-dd2d017b7ea9","source":"import",
    "import_row_id":"3c58ef3e-1cd4-486a-a9fa-af91a845c675","op_id":"OP-000866",
    "code":"010460640976874721fR8UzN","op_type":"sell","ts_raw":"2026-09-09T11:34:32+03:00",
    "actor":"op-shop-08","initiator":"qc","role":"A3","decision":"rejected","reason":"TIME_REVERSED",
    "message":"Время операции не позже последней принятой операции по коду: 2026-09-09T08:34:32Z не позже 2026-09-09T09:07:45Z",
    "mock_result":null,"rules_version":"2026.11-1","created_at":"2026-11-16T15:51:25Z"},
   {"op_id":"OP-000113","decision":"duplicate","…":"повторный импорт"}],
 "violations":[{"reason":"TIME_REVERSED","source":"operations","ref":"OP-000866","state":"open","…":"…"}]}
```

### 5.5. Расхождения

**`GET /api/violations`** — параметры: `reason`, `source` (`items / codes / operations`), `state` (`open / closed`, иное → 400), `item_code`, `actor`, `from`/`to` (по `created_at`, форматы как в 5.4), `q` (подстрока в коде, `ref` или сообщении), `limit`/`offset` (100 / 0, до 5000). Сортировка — новые сверху. `counts` по причинам учитывает все фильтры, кроме `reason`. Ответ 200 (стенд, `?limit=1`):

```json
{"rows":[{"violation_id":"2cc708e8-0487-47d7-adb0-b53668d5c24b","source":"codes",
          "ref":"010469355113939621m5ygCa","code":"010469355113939621m5ygCa","item_code":"ITM-0028",
          "reason":"SNAPSHOT_MISMATCH",
          "message":"Итоговое состояние кода не совпадает с ожидаемым: ожидалось статус sold, ts 2026-10-01T06:42:24Z; фактически статус introduced, ts 2026-10-01T06:42:24Z",
          "attempt_id":null,"import_row_id":null,"rules_version":"2026.11-1",
          "created_at":"2026-11-16T15:51:25Z","op_ts":null,"actor":null,
          "state":"open","resolutions":[]}],
 "total":18,
 "counts":{"CODE_FORMAT":2,"CODE_NOT_FOUND":1,"DUPLICATE_CODE":2,"GTIN_CHECKSUM":2,"GTIN_FORMAT":1,
           "GTIN_MISMATCH":1,"IDEMPOTENCY_CONFLICT":1,"ITEM_NOT_FOUND":1,"SNAPSHOT_MISMATCH":2,
           "TIME_REVERSED":1,"TRANSITION_NOT_ALLOWED":3,"VALUE_FORMAT":1}}
```

`ref` — ключ источника: `item_code` для items, код для codes, `op_id` для operations.

**`GET /api/violations/export.csv`** — те же фильтры, без пагинации; формат `golden/violations.csv`: заголовок `code,reason,source,ref`, сортировка source (items < codes < operations), затем ref, затем reason. Стенд (начало):

```csv
code,reason,source,ref
,GTIN_CHECKSUM,items,ITM-0097
,GTIN_CHECKSUM,items,ITM-0098
,GTIN_FORMAT,items,ITM-0099
(01)04660761090717(21)LKNJq7,CODE_FORMAT,codes,(01)04660761090717(21)LKNJq7
```

**`POST /api/violations/{id}/resolutions`** — роли A3, admin. Тело `{action: "comment" | "close", comment: непустая строка}`. Ответ 201 (по контракту):

```json
// запрос пользователя qc
{"action":"close","comment":"Ожидаемый статус в codes.csv ошибочен, учётная система исправит выгрузку"}
// ответ
{"id":"6f0b…","violation_id":"2cc708e8-0487-47d7-adb0-b53668d5c24b","action":"close",
 "comment":"Ожидаемый статус в codes.csv ошибочен, учётная система исправит выгрузку",
 "author":"qc","created_at":"2026-11-16T16:20:00Z"}
```

Ошибки: 400 `VALUE_FORMAT` (неизвестное действие), 400 `REQUIRED_FIELD` (пустой комментарий), 404 (нет такого расхождения или id не UUID), 409 (уже закрыто). Комментарии и закрытие не редактируются и не удаляются.

### 5.6. Импорт

**`POST /api/imports`** — роли A1, A3, admin; `multipart/form-data` с полями `items`, `codes`, `operations` (любое непустое подмножество). Форматы файлов — SPEC §2.1: UTF-8, разделитель запятая, строка заголовка обязательна и совпадает точно:

| Поле | Заголовок |
|---|---|
| `items` | `item_code,name,gtin` |
| `codes` | `code,item_code,status,ts` |
| `operations` | `op_id,code,op_type,ts,actor` |

Запрос:

```bash
curl -b jar -F items=@data/C16/T1/items.csv -F codes=@data/C16/T1/codes.csv \
     -F operations=@data/C16/T1/operations.csv http://localhost:8080/api/imports
```

Ответ 200 — сессия со счётчиками, отклонёнными строками и числом новых расхождений проверки снимка. Первый импорт T1 (стенд, `rejected_rows` сокращены с 16 до 2):

```json
{"session_id":"8ed24287-c565-44a0-96d5-6f031e0f84d6","started_by":"qc","role":"A3",
 "started_at":"2026-11-16T15:51:24Z","finished_at":"2026-11-16T15:51:25Z",
 "status":"completed","failure_reason":null,
 "files":[{"entity":"items","filename":"items.csv","size":10800,"sha256":"db955e64…d44570"},
          {"entity":"codes","filename":"codes.csv","size":35064,"sha256":"10e6067f…52c4b3"},
          {"entity":"operations","filename":"operations.csv","size":68682,"sha256":"2c48531b…233eeeb"}],
 "counters":{"items":{"accepted":96,"duplicate":0,"rejected":3},
             "codes":{"accepted":512,"duplicate":0,"rejected":6},
             "operations":{"accepted":862,"duplicate":1,"rejected":7}},
 "rows_created":1488,
 "rejected_rows":[
   {"row_id":"29519974-5f55-4b78-a566-9197742733ff","entity":"items","line_no":98,"key":"ITM-0097",
    "raw":["ITM-0097","Кефир 1%, 900 г, ТМ «Северная ферма»","4626734562218"],
    "processing_status":"rejected","reason":"GTIN_CHECKSUM",
    "message":"Неверная контрольная цифра GTIN: «4626734562218»"},
   {"row_id":"dea718dc-d2a1-4cdd-a03e-e9af95044025","entity":"operations","line_no":870,"key":"OP-000800",
    "raw":["OP-000800","010469355111532121SOyEMV","sell","2026-10-20T12:47:08+03:00","op-shop-07"],
    "processing_status":"rejected","reason":"IDEMPOTENCY_CONFLICT",
    "message":"Повтор ключа с другим содержимым: операция OP-000800 уже принята с другим содержимым"}],
 "snapshot_violations":2}
```

`rows_created` = новые строки в `items`, `code_registry`, `operations` и `violations`: 96 + 512 + 862 + 16 отклонений + 2 расхождения снимка = 1488. Повторный импорт тех же файлов (стенд): все 1487 строк — `duplicate`, `rows_created: 0`.

Ошибка структуры не даёт HTTP-ошибки: ответ 200 с `"status":"structure_rejected"` и `failure_reason`, например «Файл items.csv: заголовок «item_code,name» не совпадает с ожидаемым «item_code,name,gtin»»; строки не обрабатываются. Технический сбой посреди сессии — `"status":"failed"`, уже принятые строки сохраняются. HTTP-ошибки: 400, 403, 413 (раздел 4).

**`GET /api/imports`** — до 200 последних сессий, новые сверху, без `rejected_rows` (пустой массив). **`GET /api/imports/{id}`** — сессия и все отклонённые строки (порядок items, codes, operations, затем номер строки); 404, если сессии нет.

### 5.7. KPI и сверка с эталоном

**`GET /api/kpi`** — 200 (стенд, после первого и повторного импорта T1):

```json
{"k1":{"accepted":862,"rejected":7,"percent":99.19,"target":99},
 "k2":{"consistent":512,"total":512,"percent":100},
 "k3":{"last_session_id":"4a7da659-730a-429d-9e26-7f661c4bed6f","rows_created":0,"reimport":true},
 "k4":{"last_ms":0.84},
 "k5":{"percent":100}}
```

K1 — доля принятых среди принятых и отклонённых попыток (повторы не учитываются); K2 — коды, у которых статус совпадает со статусом последней принятой операции; K3 — `rows_created` последнего повторного импорта тех же файлов (по sha256), иначе последней завершённой сессии; K4 — длительность последнего `GET /codes`, `null` до первого запроса после старта; K5 — процент совпадения с golden, `null`, если эталон недоступен.

**`GET /api/golden/compare`** — 200 (стенд):

```json
{"status_summary":{"match":true,"expected_rows":5,"actual_rows":5,"missing":[],"extra":[]},
 "violations":{"match":true,"expected_rows":18,"actual_rows":18,"missing":[],"extra":[]},
 "percent":100}
```

`missing` — строки эталона, которых нет в текущей выгрузке; `extra` — лишние строки. Эталон сравнивается с состоянием «чистая БД + один импорт T1»; после демонстрационных операций совпадение уменьшается — это ожидаемо.

### 5.8. Проверка кода и служебные

**`POST /api/verify`** — тело `{code, gtin?}`; backend пересылает его в мок (таймаут 2 с) и возвращает тело мока как есть. Если мок не ответил корректно — `{"is_valid":false,"reason":"MOCK_UNAVAILABLE","message":"Мок-сервис ЦРПТ недоступен"}`. HTTP-статус всегда 200. Стенд:

| Запрос | Ответ |
|---|---|
| `{"code":"010468913592648321BKVhBt","gtin":"04689135926483"}` | `{"is_valid":true}` |
| `{"code":"010468913592648321ZZZZZZ"}` | `{"is_valid":false,"reason":"UNKNOWN_CODE"}` |
| `{"code":"(01)04689135926483(21)BKVhBt"}` | `{"is_valid":false,"reason":"INVALID_FORMAT"}` |
| `{"code":"010468913592648321BKVhBt","gtin":"4663468584466"}` | `{"is_valid":false,"reason":"GTIN_MISMATCH"}` |

**`GET /api/healthz`** (и `GET /healthz`) — без входа, 200 `{"status":"ok"}`.

## 6. Идемпотентность и повторы

Отдельного заголовка `Idempotency-Key` нет: его роль выполняют бизнес-ключи.

| Ключ | Где | Повтор с тем же содержимым | Тот же ключ, другое содержимое |
|---|---|---|---|
| `op_id` | форма и `operations.csv` | `duplicate`, новой операции нет | `IDEMPOTENCY_CONFLICT` |
| `(code, ts)` | форма и `operations.csv` | тот же `op_type` и `actor` → `duplicate` даже с другим `op_id` | `IDEMPOTENCY_CONFLICT` |
| `item_code` | `items.csv` | `duplicate` | `IDEMPOTENCY_CONFLICT` |
| `code` | `codes.csv` | `duplicate` | `DUPLICATE_CODE` |
| ранее отклонённая строка или попытка с тем же сырым содержимым | импорт и форма | `duplicate`, второго расхождения нет | — |

Правила повтора для клиента:

1. Чтобы безопасно повторить операцию после таймаута или 500, клиент должен сам сгенерировать `op_id` и отправить его повторно. Без `op_id` сервер создаёт новый UUID, и повтор будет новой попыткой. Две одинаковые операции без `ts` в одну секунду совпадут по `(code, ts)` и дадут `duplicate`.
2. Отказ `MOCK_UNAVAILABLE` не окончательный: повтор с тем же `op_id` после восстановления мока проверяется заново. Так же не окончательны `CODE_NOT_FOUND` (если код позже появился) и `ITEM_NOT_FOUND` для строки `codes.csv` (если товар позже загружен).
3. Каждый запрос `POST /operations`, дошедший до проверки правил (то есть не отклонённый с 400, 401, 403 и не завершившийся 500), оставляет строку в `operation_attempts`, в том числе при повторе (`decision = duplicate`). Предметные таблицы при повторе не меняются.
4. Повторный импорт тех же файлов даёт `rows_created = 0` (KPI K3); новые расхождения снимка не создаются, даже если прежние уже закрыты.
5. `POST /violations/{id}/resolutions` не идемпотентен: каждый `comment` добавляет запись, второй `close` → 409.
6. Конкурентные запросы по одному коду или `op_id` упорядочиваются advisory-блокировками PostgreSQL; состояние кода перепроверяется после ответа мока.

## 7. Мок-сервис ЦРПТ: `/cz/verify`

Сервис `mock` заменяет проверку кода в ГИС МТ; боевой интеграции нет по условию кейса. Адрес в сети compose — `http://mock:8080`, на хосте — `http://localhost:8082`. Сервис без состояния, кроме режима работы.

| Метод и путь | Запрос | Ответ |
|---|---|---|
| `POST /cz/verify` | `{"code": "...", "gtin": "..."}`; `gtin` необязателен (пустой или `null` = не передан) | 200 `{"is_valid": true}` или `{"is_valid": false, "reason": "INVALID_FORMAT \| UNKNOWN_CODE \| GTIN_MISMATCH"}`; битый JSON → 400 `{"error":"BAD_REQUEST",…}` |
| `GET /admin/mode` | — | `{"mode":"normal"}` |
| `POST /admin/mode` | `{"mode":"normal \| slow \| error \| garbage"}` | 200 `{"mode":…}`; иной режим → 400 `{"error":"BAD_MODE","message":"Режим должен быть одним из: normal, slow, error, garbage"}` |
| `GET /healthz` | — | `{"mode":"normal","registry_size":542,"status":"ok"}` в любом режиме |

Порядок проверок: маска `^01(\d{14})21([A-Za-z0-9]{6})$` → наличие кода в реестре `mock_registry.csv` → совпадение нормализованного GTIN (GTIN-13 дополняется ведущим нулём) с GTIN внутри кода.

### 7.1. Управляемые сценарии ошибок

Режим задаётся переменной `MOCK_MODE` при старте или `POST /admin/mode` на лету и влияет только на `/cz/verify`. Ответы сняты с отдельного экземпляра мока, собранного из `mock/` (16.11.2026).

| Режим | Ответ мока | Как его видит backend | Итог операции формы | `operation_attempts.mock_result` |
|---|---|---|---|---|
| `normal` | 200 `{"is_valid":true}` или `false` + `reason` | ответ получен | `accepted` или `rejected` `MOCK_INVALID` | тело мока |
| `slow` | пауза `MOCK_SLOW_MS` (5000 мс), затем обычный ответ; клиент уходит через 2 с, в журнале мока статус 499 | таймаут 2 с | `rejected` `MOCK_UNAVAILABLE` | `{"is_valid":false,"reason":"MOCK_UNAVAILABLE","detail":"…deadline exceeded…"}` |
| `error` | 500 `{"error":"MOCK_ERROR","message":"Имитация внутренней ошибки сервиса проверки"}` | не 200 | `rejected` `MOCK_UNAVAILABLE` | `{…,"detail":"HTTP 500"}` |
| `garbage` | 200 `{}` | нет булева `is_valid` | `rejected` `MOCK_UNAVAILABLE` | `{…,"detail":"нет поля is_valid"}` |

Во всех сбойных режимах код не меняет статус, ответ формы содержит понятную причину, а `POST /api/verify` возвращает `MOCK_UNAVAILABLE`. Импорт мок не вызывает и от режима не зависит. Сценарий переключения режимов автоматизирован в `scripts/demo.sh`.

## 8. Сценарии взаимодействия

Диаграммы последовательности — [seq-operation.png](diagrams/seq-operation.png) и [seq-import.png](diagrams/seq-import.png), пошаговые таблицы — [27-architecture.md](27-architecture.md), раздел 7.

### S1. Операция с подтверждением мока (UC1–UC4, нормальный поток)

Участники: SPA, backend, mock, PostgreSQL. Предусловие: пользователь вошёл, роль подходит к типу операции.

1. SPA → backend: `POST /api/operations {op_type, code, item_code?, ts?}` с cookie.
2. Backend проверяет сеанс, роль, правила 1–10 по текущему состоянию кода.
3. Backend → mock: `POST /cz/verify {code, gtin}`; mock → backend: `{"is_valid":true}`.
4. Backend в одной транзакции перепроверяет состояние под блокировкой и записывает попытку, операцию и проекцию кода.
5. Backend → SPA: 200 `{decision:"accepted", code:{status,…}}`; SPA показывает новый статус.

Альтернативы: правило не пройдено → шаг 3 пропускается, `rejected` с причиной и расхождение; мок ответил `false` → `MOCK_INVALID`; мок в режиме `slow / error / garbage` → `MOCK_UNAVAILABLE`, статус не меняется; состояние изменилось за время ответа мока → решение по новому состоянию; роль не подходит → 403 без записи.

### S2. Импорт выгрузки учётной системы (UC7)

1. A3 выбирает три файла; SPA → backend: `POST /api/imports` (multipart).
2. Backend создаёт сессию, проверяет структуру всех файлов, затем обрабатывает строки по одной в транзакции, без вызова мока.
3. После строк — проверка снимка `codes.csv` против принятой истории.
4. Backend → SPA: сессия со счётчиками и отклонёнными строками; SPA показывает их и ссылки на расхождения.

Альтернативы: ошибка заголовка или кодировки → `structure_rejected`, данные не меняются; повтор тех же файлов → все строки `duplicate`, `rows_created = 0`; техническая ошибка → `failed`, принятые строки остаются.

### S3. Разбор расхождения (UC5)

1. A3 открывает журнал: `GET /api/violations?state=open&reason=…`.
2. Переходит в историю кода: `GET /api/codes/{code}`.
3. Добавляет комментарий или закрывает: `POST /api/violations/{id}/resolutions` → 201; повторное закрытие → 409.
4. Аудитор видит то же самое, кнопок изменения у него нет; запись вернула бы 403.

### S4. Выгрузка статусов во внешнюю систему (UC6)

1. A3 или A4 задаёт фильтры: `GET /api/codes?…`.
2. Выгружает CSV: `GET /api/codes/export.csv?…` (`code,item_code,status,last_ts`), передаёт файл в учётную систему E1.
3. Для сверки с эталоном — `GET /api/status-summary.csv` и `GET /api/violations/export.csv` в формате `golden/*.csv`.

## 9. Отчёт по интеграции

### 9.1. Область и участники

| Участник | Роль в интеграции | Канал | Владелец |
|---|---|---|---|
| SPA (`web`) | Клиент REST API | HTTP JSON через nginx | Жраков Артём (frontend) |
| Backend (`backend`) | Поставщик REST API, клиент мока, владелец БД | HTTP, SQL | Брусникин Максим (backend) |
| Мок ЦРПТ (`mock`) | Поставщик проверки кода | HTTP JSON | Брусникин Максим (backend) |
| Учётная система заказчика (E1) | Источник CSV T1 и получатель выгрузки статусов; в MVP — файлы `data/C16/T1/*.csv` | файлы CSV | учебная роль, заказчик кейса |
| ГИС МТ «Честный знак» (E3) | Не подключается по условию кейса | — | — |

Контроль полноты документации — Шиншин Алексей (тимлид); требования к интеграции и данные для проверки — Завгороднев Ярослав (аналитик).

Не входит в MVP: боевая ГИС МТ, асинхронные события, очередь повторов, токены Bearer/OAuth2, HTTPS.

### 9.2. Контракты и модели данных

Все эндпойнты — раздел 3; схемы запросов и ответов — `components.schemas` в [openapi.yaml](openapi.yaml). Форматы CSV обмена — раздел 5.6 и SPEC §2.1, §7. Соответствие полей CSV, API и таблиц — [13-data-dictionary.md](13-data-dictionary.md) и SPEC §4. Политика изменений: новые поля только добавляются; изменение правил повышает `rules_version`, которая хранится в каждой попытке и расхождении.

### 9.3. Качество, надёжность, безопасность

| Требование | Реализация в MVP |
|---|---|
| Время ответа мока | Таймаут клиента 2 с; дольше — `MOCK_UNAVAILABLE`, без зависания формы |
| Повторы | Автоматических повторов нет; безопасный ручной повтор через тот же `op_id` (раздел 6) |
| Идемпотентность и дедупликация | Бизнес-ключи `op_id`, `(code, ts)`, `item_code`, `code`; повторный импорт — 0 новых строк |
| Согласованность | Одна транзакция на операцию или строку импорта; advisory-блокировки; перепроверка после ответа мока |
| Аутентификация | Пароль (bcrypt) → cookie-сеанс 12 ч, HttpOnly, SameSite=Lax |
| Авторизация | Роли A1–A4, admin на каждом эндпойнте; запись запрещена аудитору (403) |
| Персональные данные | Хранятся только логины и отображаемые имена демо-пользователей; пароли — только хешем |
| Ограничения размера | JSON 1 МБ, импорт 20 МБ, nginx 50 МБ |
| Транспорт | HTTP без TLS на учебном стенде; CORS не нужен — SPA и API на одном origin через nginx |

### 9.4. Мониторинг и обработка ошибок

| Источник | Что пишется |
|---|---|
| Backend | Одна строка на запрос: метод, путь, HTTP-статус, длительность (кроме `/healthz`); полные тексты внутренних ошибок и паники со стеком |
| Mock | Одна JSON-строка на запрос: method, path, status (499 — клиент ушёл), duration_ms, mode, code, gtin, is_valid, reason |
| Данные | Каждая попытка с `mock_result`; каждая сессия импорта со статусом, `failure_reason`, счётчиками |
| Готовность | `GET /healthz` backend и mock; healthcheck контейнеров `db` и `backend` |

Сквозного идентификатора запроса (`X-Request-Id`) нет; связь между записями — через `attempt_id`, `op_id`, `session_id`, `import_row_id`. Журналы смотрятся командой `make logs`.

### 9.5. Тестирование и приёмка

| Проверка | Что подтверждает | Результат 16.11.2026 |
|---|---|---|
| `backend/internal/httpapi/api_test.go` | Сквозной сценарий API: вход, роли, форма, импорт, отчёты, разбор | проходит в `make test` |
| `backend/internal/httpapi/golden_test.go` | Импорт T1 даёт ровно эталонные `status_summary.csv` и `violations.csv` | K5 = 100 % |
| `backend/internal/ops/form_test.go` | Роли, привязка `issue`, сбои мока `error / garbage / slow` | проходит |
| `mock/server_test.go` | Порядок проверок, все режимы, `/admin/mode`, журнал | проходит |
| `scripts/verify.sh` (`make verify`) | Чистый стек, импорт T1, golden 100 %, повторный импорт `rows_created = 0`, 403 для аудитора, доступность web | K1 99,19 %; K2 100 %; K3 0; K5 100 % |
| `scripts/demo.sh` (`make demo`) | Сценарий D16 через API: issue → introduce → sell → return, отказ перехода, 403 по роли, мок `error` и `garbage`, восстановление | все шаги с ожидаемым решением |

Критерий готовности к защите: `make verify` завершается без ошибок, `GET /api/golden/compare` → `percent: 100` на чистой БД.

### 9.6. Тестовые данные для обмена

| Файл | Строк данных | Назначение | Ожидаемый результат импорта |
|---|---|---|---|
| `data/C16/T1/items.csv` | 99 | Товары; 2 ошибки контрольной цифры, 1 ошибка формата GTIN | 96 принято, 3 отклонено |
| `data/C16/T1/codes.csv` | 518 | Привязки и ожидаемый снимок; 2 `CODE_FORMAT`, 2 `DUPLICATE_CODE`, 1 `GTIN_MISMATCH`, 1 `ITEM_NOT_FOUND`; 2 намеренно неверных снимка | 512 принято, 6 отклонено, 2 `SNAPSHOT_MISMATCH` |
| `data/C16/T1/operations.csv` | 870 | История операций; 7 ошибочных строк и 1 точный повтор в конце файла | 862 принято, 7 отклонено, 1 повтор |
| `data/C16/mock_registry.csv` | 542 | Реестр мока: 512 валидных кодов T1 + 30 демо-кодов | — |
| `data/C16/demo_codes.csv` | 30 | Новые коды для живой демонстрации через форму (`code,item_code`) | — |
| `golden/status_summary.csv` | 5 | issued 300, introduced 110, sold 78, returned 12, disposed 12 | совпадение 5 из 5 |
| `golden/violations.csv` | 18 | Эталонные расхождения в формате `code,reason,source,ref` | совпадение 18 из 18 |

Данные детерминированно создаются генератором `tools/gen` (сид 16, `make gen`) независимо от кода backend.

### 9.7. План релиза

Выкатка — `make up` (сборка образов и старт), миграции применяются автоматически при старте backend. Откат — остановка стека и сборка предыдущего тега; `make reset` удаляет том БД, после чего стенд заново наполняется импортом T1. Репозиторий: [ссылка на репозиторий GitVerse].

### 9.8. Отклонения от практики 25–26

| Рекомендация практики | В MVP | Обоснование |
|---|---|---|
| Версия в URI `/api/v1` | Нет | Один клиент, API в одном репозитории с SPA; версия — в OpenAPI и `rules_version` |
| Bearer-токен | Cookie-сеанс | SPA на том же origin; HttpOnly-cookie не доступен JavaScript |
| 422 для ошибок валидации | 200 с `decision: rejected` для бизнес-отказов, 400 для неверного запроса | Отказ операции — нормальный результат, он сохраняется в журнале |
| `Idempotency-Key` | `op_id` в теле | Ключ уже есть в предметной модели и в CSV |
| HTTPS, rate limiting, X-Request-Id | Нет | Локальный учебный стенд |
