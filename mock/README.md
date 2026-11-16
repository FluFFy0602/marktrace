# mock — имитация сервиса проверки кода «Честный знак»

Go-модуль `marktrace/mock` (только stdlib), SPEC §8.

| Метод и путь | Ответ |
|---|---|
| `POST /cz/verify {code, gtin?}` | 200 `{"is_valid":true}` или `{"is_valid":false,"reason":"INVALID_FORMAT\|UNKNOWN_CODE\|GTIN_MISMATCH"}`; битый JSON → 400 `BAD_REQUEST` |
| `GET /admin/mode` | `{"mode":"normal"}` |
| `POST /admin/mode {mode}` | переключение режима на лету; неизвестный режим → 400 `BAD_MODE` |
| `GET /healthz` | `{"status":"ok","mode":…,"registry_size":…}` (200 в любом режиме) |

Порядок проверок: маска `^01(\d{14})21([A-Za-z0-9]{6})$` → наличие в реестре →
сравнение GTIN (GTIN-13 дополняется ведущим нулём; пустой или `null` gtin = не передан).

Режимы (влияют только на `/cz/verify`): `normal`; `slow` — пауза `MOCK_SLOW_MS`
(по умолчанию 5000 мс, бэкенд падает по таймауту 2 с), затем обычный ответ; `error` — HTTP 500;
`garbage` — 200 с телом `{}`.

Переменные окружения: `ADDR` (`:8080`), `REGISTRY_FILE` (`/data/mock_registry.csv`,
заголовок `code`), `MOCK_MODE` (`normal`), `MOCK_SLOW_MS` (`5000`). На каждый запрос — одна
JSON-строка лога в stdout (method, path, status, duration_ms, mode, code, is_valid, reason).

```sh
go test ./...
ADDR=:8082 REGISTRY_FILE=../data/C16/mock_registry.csv go run .
docker build -t marktrace-mock . && \
  docker run -p 8082:8080 -v "$PWD/../data/C16:/data:ro" marktrace-mock
curl -s -XPOST localhost:8082/admin/mode -d '{"mode":"slow"}'
```
