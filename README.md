# ticktick-cli

Go-клиент и CLI к TickTick Open API — замена официального TickTick MCP-сервера,
который был ограничен по функциональности, медленным и дорогим по токенам.
Спецификация составлена вручную по официальной документации (машинно-читаемой
OpenAPI-спеки у TickTick не существует), клиент сгенерирован `oapi-codegen`,
а CLI `ticktick` покрывает все бизнес-сущности API и рассчитан на скрипты
и AI-скиллы: компактный текстовый вывод по умолчанию и сырой JSON по флагу `--json`.

Ключевые свойства:

- **Полное покрытие API** — 46 операций документации в 44 схемах, 47 команд CLI
  (46 API-операций + клиентский батч комментариев).
- **`--json` в каждой команде** — persistent-флаг корня, печатает исходный JSON
  ответа API в stdout, без переупаковки и без потери полей.
- **Пример ответа API в `--help` каждой команды** — блок `Example API response`.
- **`task comments-batch`** — фирменная команда: в API нет батч-эндпоинта
  комментариев, клиент собирает `map[taskId][]comments` сам, 1–8 параллельных
  воркеров (`--workers`, по умолчанию 4).
- **Надёжность** — 110 тестов на `httptest` (включая прогон под `-race`),
  `go vet` без замечаний, плюс live smoke-тест против реального API
  (`scripts/smoke.sh`).

## Установка

Требования: Go 1.23+ (в `go.mod` указан `go 1.23`), `make`.

```bash
git clone <repo> ticktick-cli && cd ticktick-cli
make build          # → ./ticktick
./ticktick --help
```

Бинарь собирается из `./cmd/ticktick`; в `.gitignore` он не отслеживается,
поэтому `make build` нужен после каждого изменения кода. Если `make` нет,
эквивалентная команда — `go build -o ticktick ./cmd/ticktick`.

### Перегенерация клиента

Клиент (`api/openapi.gen.go`) генерируется из `api/openapi.yaml` утилитой
`oapi-codegen` **v2.4.1** (types + client):

```bash
go install github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.4.1
make generate       # oapi-codegen -package api -generate types,client
make build
```

Фрагменты спеки (`api/frag-*.yaml`) склеиваются в `api/openapi.yaml` скриптом
`scripts/merge-spec.sh`; подробности — в разделе «Архитектура».

## Авторизация

Используется персональный API-токен TickTick (OAuth-флоу в CLI не реализован,
см. «Ограничения»).

1. Откройте [TickTick web app](https://ticktick.com/webapp/).
2. Аватар слева сверху → **Settings** → **Account** → **API Token**.
3. Создайте и скопируйте токен.

Порядок разрешения токена (первый непустой источник побеждает):

1. флаг `--token`;
2. переменная окружения `TICKTICK_TOKEN`;
3. файл `~/.config/ticktick/token`.

```bash
# вариант 1: файл (рекомендуется для интерактивной работы)
mkdir -p ~/.config/ticktick
printf '%s' "$TOKEN" > ~/.config/ticktick/token
chmod 600 ~/.config/ticktick/token

# вариант 2: переменная окружения (удобно для CI/скриптов)
export TICKTICK_TOKEN="$TOKEN"

# вариант 3: разовый флаг
./ticktick project list --token "$TOKEN"
```

Прочие глобальные флаги (доступны в любой команде):

| Флаг | Переменная | По умолчанию | Назначение |
|---|---|---|---|
| `--token string` | `TICKTICK_TOKEN` | — | API-токен |
| `--base-url string` | `TICKTICK_BASE_URL` | `https://api.ticktick.com` | базовый URL API |
| `--timeout duration` | — | `30s` | таймаут HTTP-запроса |
| `--json` | — | `false` | сырой JSON ответа в stdout |

> **Безопасность.** Не коммитьте токен: `.gitignore` уже исключает `.env`,
> `*.token` и `token`. Файл `~/.config/ticktick/token` держите с правами `600`.
> Токен даёт полный доступ к аккаунту, включая удаление задач и проектов.

## Формат вывода

Контракт одинаков для всех команд:

| Режим | stdout | Пример |
|---|---|---|
| по умолчанию | компактный человекочитаемый текст: таблицы (проекты — `ID NAME KIND VIEW CLOSED GROUP`, задачи — `ID TITLE STATUS PRIORITY DUE PROJECT`) и пары «ключ: значение» | `ticktick project list` |
| `--json` | сырой JSON ответа API, отформатированный с отступом 2, без изменений схемы | `ticktick project list --json` |
| пустой ответ (delete/complete) | текст в текстовом режиме, `{"ok":true}` в JSON-режиме | `ticktick task delete <id> --project <p> --json` |
| `task comments-batch --json` | чистая карта `{taskId: [comments]}`, собранная клиентом | см. раздел «Батч комментариев» |

Ошибки всегда идут в **stderr** и завершают процесс с кодом **1**:

```console
$ ./ticktick project get does-not-exist
Error: API error: HTTP 500: {"errorMessage":"..."}
$ echo $?
1
```

Коды возврата: `0` — успех; `1` — ошибка API, сети, аргументов или частичный
сбой батча. При не-2xx ответе в сообщении есть HTTP-статус и тело ответа
(обрезается до 300 символов).

В `--help` **каждой** команды есть блок `Example API response` с реальным
примером ответа (для пустых ответов — пометка `empty body, HTTP 200`):

```bash
./ticktick task create --help | sed -n '/Example API response/,/^$/p'
```

## Быстрый старт

```bash
# 1. Список проектов: человекочитаемая таблица
./ticktick project list

# 2. id и имена проектов — JSON + jq
./ticktick project list --json | jq -r '.[] | [.id, .name] | @tsv'

# 3. Создать задачу с приоритетом и тегом
./ticktick task create --project 6226ff9877acee87727f6bca \
  --title "Купить молоко" --due 2026-01-03 --priority 3 --tag shopping

# 4. Незавершённые задачи инбокса за неделю (диапазон ≤ 14 дней)
./ticktick task undone --start 2026-11-14 --end 2026-11-20 --projects inbox

# 5. Комментарии пачки задач одного проекта (форма 1)
./ticktick task comments-batch --project 6226ff9877acee87727f6bca \
  --ids 641a2b3c4d5e6f7890ab01,641a2b3c4d5e6f7890ab02 --workers 8 --json

# 6. То же для задач из разных проектов (форма 2)
./ticktick task comments-batch --tasks \
  6226ff9877acee87727f6bca:641a2b3c4d5e6f7890ab01,63f1a2b3c4d5e6f7890abcdf:641a2b3c4d5e6f7890ab02 --json

# 7. Фокус-сессии за апрель (тип 0 — Pomodoro, 1 — Timing)
./ticktick focus list --from 2026-04-01 --to 2026-04-30 --type 0
```

Даты принимаются в удобном виде — `YYYY-MM-DD`, `"YYYY-MM-DD HH:MM"` или
RFC3339 — и нормализуются в формат API (`yyyy-MM-dd'T'HH:mm:ssZ`). Даты со
временем берите в кавычки.

## Справочник команд

Всего 47 команд API (46 операций + `comments-batch`) плюс служебные `version`,
`help` и автогенерируемый `completion`. Позиционные аргументы показаны
заглавными (`TASK_ID`), `*` — обязательный флаг (проверяется cobra либо самим
обработчиком команды). Точный набор флагов и их обязательность всегда печатает
`--help` соответствующей команды.

### task — задачи (18)

| Команда | Назначение | Аргументы и ключевые флаги |
|---|---|---|
| `task get` | получить задачу по id | `TASK_ID --project*` |
| `task create` | создать задачу | `--title* --project*` + общие флаги задачи |
| `task update` | изменить задачу (меняются только переданные флаги) | `TASK_ID --project*` + общие флаги задачи |
| `task complete` | завершить задачу | `TASK_ID --project*` |
| `task complete-many` | завершить до 50 задач одним запросом | `--ids* [--project]` (без `--project` — инбокс) |
| `task delete` | удалить задачу | `TASK_ID --project*` |
| `task move` | переместить задачи между проектами | `--from --to --ids` или `--data` (одно из двух) |
| `task batch` | батч-создание и обновление (≤50 add, ≤50 update) | `--data` или `--file` (одно из двух) |
| `task assign` | назначить исполнителя из участников проекта | `TASK_ID --project* --username*` |
| `task unassign` | снять исполнителя | `TASK_ID --project*` |
| `task completed` | завершённые задачи в диапазоне `completedTime` (≤200) | `--projects --start --end` |
| `task filter` | фильтр по проектам, `startDate`, приоритету, тегам, kind, статусу (≤200) | `--projects --start --end --priority --tags --kind --status` |
| `task undone` | незавершённые задачи, диапазон ≤14 дней | `--start* --end* [--projects] [--ids]` |
| `task search` | поиск по ключевым словам + фильтры | `KEYWORDS* [--projects --tags --status --due-from --due-to]` |
| `task comments` | комментарии задачи | `TASK_ID --project*` |
| `task comment-add` | добавить комментарий | `TASK_ID --project* --text*` |
| `task comment-delete` | удалить комментарий | `TASK_ID COMMENT_ID --project*` |
| `task comments-batch` | комментарии для многих задач (1–8 воркеров) | `--project --ids` или `--tasks`; `[--workers]` |

Общие флаги `task create` / `task update` (полный набор):
`--all-day`, `--content`, `--data`, `--desc`, `--due`, `--item` (повторяемый),
`--kind`, `--parent`, `--priority`, `--project`, `--reminder` (повторяемый),
`--repeat`, `--repeat-from`, `--sort-order`, `--start`, `--tag` (повторяемый),
`--title`, `--tz`.
Флаг `--data` принимает сырое JSON-тело: поля из него мержатся первыми,
явно переданные флаги имеют приоритет.

Значения: `--priority` — `0` нет, `1` низкий, `3` средний, `5` высокий;
`--kind` — `TEXT`, `NOTE`, `CHECKLIST`; `--status` (в `filter`/`search`) —
`-1` заброшено, `0` открыто, `2` завершено; `--priority`, `--tags`, `--kind`,
`--status`, `--projects` принимают списки через запятую. Для `task undone`
значение `--projects inbox` означает инбокс.

### project — проекты (7)

| Команда | Назначение | Аргументы и ключевые флаги |
|---|---|---|
| `project list` | список проектов | `[--limit 200] [--offset N]` |
| `project get` | один проект по id | `PROJECT_ID` |
| `project data` | проект + незавершённые задачи + колонки | `PROJECT_ID` |
| `project members` | участники проекта | `PROJECT_ID` |
| `project create` | создать проект | `--name* [--color --sort-order --view-mode --kind]` |
| `project update` | изменить проект (только переданные флаги) | `PROJECT_ID [--name --color --sort-order --view-mode --kind]` |
| `project delete` | удалить проект | `PROJECT_ID` |

`--view-mode` — `list`, `kanban`, `timeline`; `--kind` — `TASK` или `NOTE`.

### group — группы проектов (4)

| Команда | Назначение | Аргументы и ключевые флаги |
|---|---|---|
| `group list` | все группы проектов | — |
| `group create` | создать группу (имя ≤64 символов) | `--name*` |
| `group update` | переименовать группу (≤64 символов) | `GROUP_ID --name*` |
| `group delete` | удалить группу | `GROUP_ID` |

### column — колонки канбана (3)

| Команда | Назначение | Аргументы и ключевые флаги |
|---|---|---|
| `column list` | колонки проекта | `--project*` |
| `column create` | создать колонку (имя ≤1000 символов) | `--project* --name*` |
| `column update` | переименовать колонку (≤1000 символов) | `COLUMN_ID --project* --name*` |

### tag — теги (2)

| Команда | Назначение | Аргументы и ключевые флаги |
|---|---|---|
| `tag list` | все теги | — |
| `tag create` | создать тег (имя в нижнем регистре, ≤64 символов; `label` должен совпадать с `name`) | `--name* --label*` |

### focus — фокус-сессии (4)

| Команда | Назначение | Аргументы и ключевые флаги |
|---|---|---|
| `focus get` | одна сессия по id | `FOCUS_ID --type*` |
| `focus list` | сессии в диапазоне (сервер обрезает диапазоны >30 дней) | `--from* --to* --type*` |
| `focus create` | создать сессию | `--type* [--task --start --end --duration --pause --note --relation --data]` |
| `focus delete` | удалить сессию | `FOCUS_ID --type*` |

`--type` — `0` Pomodoro, `1` Timing.

### countdown — счётчики (1)

| Команда | Назначение | Аргументы и ключевые флаги |
|---|---|---|
| `countdown list` | все счётчики | — |

### habit — привычки (7)

| Команда | Назначение | Аргументы и ключевые флаги |
|---|---|---|
| `habit list` | все привычки | — |
| `habit get` | привычка по id | `HABIT_ID` |
| `habit sections` | секции привычек пользователя | — |
| `habit create` | создать привычку (имя ≤1000 символов) | `--name*` + флаги привычки |
| `habit update` | изменить привычку (только переданные флаги) | `HABIT_ID` + флаги привычки |
| `habit checkin` | отметить выполнение за одну дату | `HABIT_ID --stamp* [--value --goal --status --time --op-time]` |
| `habit checkins` | история отметок для списка привычек | `--ids* --from* --to*` |

Флаги `habit create` / `habit update`:
`--color`, `--completed-cycles`, `--data`, `--encouragement`, `--ex-date`
(повторяемый), `--goal`, `--icon`, `--name`, `--record`, `--reminder`
(повторяемый), `--repeat`, `--section`, `--sort-order`, `--status`, `--step`,
`--style`, `--target-days`, `--target-start`, `--type`, `--unit`.
Даты отметок и диапазонов — `YYYYMMDD` или `YYYY-MM-DD`.

### preference — настройки пользователя (1)

| Команда | Назначение | Аргументы и ключевые флаги |
|---|---|---|
| `preference` | настройки пользователя (часовой пояс) | — |

### Служебные

| Команда | Назначение |
|---|---|
| `version` | версия CLI (`ticktick-cli 1.0.0`) |
| `help` | справка по любой команде |
| `completion` | автодополнение для bash/zsh/fish/powershell |

## Батч комментариев

`task comments-batch` — главная причина существования этого CLI: в TickTick API
нет эндпоинта «комментарии для списка задач», есть только
`GET /open/v1/project/{projectId}/task/{taskId}/comments` для одной задачи.
Команда сама опрашивает задачи параллельно и собирает карту
`taskId → []comments`.

### Две формы ввода (взаимоисключающие)

```bash
# форма 1: один проект + список id
./ticktick task comments-batch --project P --ids t1,t2,t3 --workers 4

# форма 2: пары PROJECT_ID:TASK_ID (можно из разных проектов)
./ticktick task comments-batch --tasks P1:T1,P2:T2 --workers 8
```

Если не передать ни одну форму или передать обе сразу, команда завершится
ошибкой аргументов (код 1).

### Воркеры

- `--workers` по умолчанию `4`;
- значение `<= 0` превращается в `4` (значение по умолчанию);
- значение `> 8` обрезается до `8`; допустимый диапазон — 1–8;
- конкурентность подтверждена тестами (`maxInFlight`, elapsed) и прогоном под
  `-race`.

### Формат JSON

`--json` печатает **чистую карту** `{taskId: [comments]}` — без обёрток,
без служебных полей, готова к `jq`. Задачи без комментариев дают пустой массив,
а не `null`:

```json
{
  "641a2b3c4d5e6f7890ab01": [
    {
      "id": "comment-1",
      "userId": 12345,
      "title": "Comment text",
      "createdTime": 1577934245000,
      "modifiedTime": 1577934245000
    }
  ],
  "641a2b3c4d5e6f7890ab02": []
}
```

Обратите внимание: `createdTime` и `modifiedTime` у комментариев — **числа,
эпоха в миллисекундах** (в документации ошибочно указаны строки; см. раздел
«Расхождения»).

В текстовом режиме вывод сгруппирован по задачам:

```text
task 641a2b3c4d5e6f7890ab01 — 1 comment(s):
  [2020-01-02 03:04] Comment text  (id: comment-1)
task 641a2b3c4d5e6f7890ab02 — 0 comment(s):
  (no comments)
```

### Частичные сбои

Сбой одной задачи не прерывает батч:

1. карта (JSON или текст) всё равно печатается в stdout;
2. диагностика по каждой упавшей задаче уходит в stderr
   (`task <id>: <ошибка>`);
3. код возврата — `1`, итоговое сообщение — `<N> of <M> tasks failed`.

Удачные задачи в карте остаются, упавшие — в неё не попадают. Это значит, что
наличие ключа в карте = задача опрошена успешно.

## Скриптинг

`--json` + `jq` закрывают почти все сценарии. Примеры ниже используют только
команды и флаги, проверенные live smoke-тестом на реальном аккаунте.

### 1. Завершить все незавершённые задачи инбокса за неделю

```bash
#!/usr/bin/env bash
set -euo pipefail

ids=$(./ticktick task undone \
        --start "$(date -u -d '-6 days' +%F)" \
        --end   "$(date -u +%F)" \
        --projects inbox --json | jq -r '.[].id' | paste -sd, -)

[ -n "$ids" ] || { echo "нечего завершать"; exit 0; }
./ticktick task complete-many --ids "$ids" --json
```

Помните: `complete-many` принимает не более 50 id за вызов.

### 2. Карта комментариев → TSV с человеческими датами

```bash
./ticktick task comments-batch --tasks "$P1:$T1,$P2:$T2" --json \
  | jq -r 'to_entries[] | .key as $task | .value[] |
           [$task, .id, (.createdTime / 1000 | strftime("%Y-%m-%d %H:%M")), .title] | @tsv'
```

Так как `createdTime` — эпоха в миллисекундах, делим на 1000 перед `strftime`.
При частичном сбое команда вернёт код 1, но карта уже будет в stdout: не
оборачивайте пайплайн в `set -o pipefail` там, где частичный результат полезен.

### 3. Батч-создание задач из файла

```bash
jq -n --arg p "$PROJECT" \
  '{add:[{title:"Задача A",projectId:$p},{title:"Задача B",projectId:$p}]}' \
  > /tmp/batch.json

./ticktick task batch --file /tmp/batch.json --json \
  | jq -r '.status, (.items[] | [.operation, .index, .taskId, (.errorCode // "ok")] | @tsv)'
```

Ответ батча содержит `status` (`complete`/`partial`) и массив `items`, поэтому
успешные и упавшие операции разбираются по отдельности.

### 4. Без jq: то же самое на node

```bash
./ticktick task comments-batch --project "$P" --ids "$T1,$T2" --json \
  | node -e 'let d="";process.stdin.on("data",c=>d+=c);process.stdin.on("end",()=>{
      const m=JSON.parse(d);
      for (const [task, cs] of Object.entries(m))
        for (const c of cs) console.log(task, c.id, c.createdTime, c.title);
    })'
```

## Расхождения live API с документацией

Все пункты проверены на живом API 2026-11-20; спека и сгенерированные типы
приведены к **живому** виду, а не к тексту документации.

1. **`OpenComment.createdTime` / `modifiedTime` — числа (эпоха, мс).**
   Документация утверждает, что это строки `yyyy-MM-dd'T'HH:mm:ssZ`; реальный
   API возвращает числа. В спеке поля описаны как `integer (int64)`.
2. **`POST /task/batch` возвращает больше, чем описано.** Живой ответ:
   `{"status":"complete"|"partial","items":[{"operation","index","taskId","etag","errorCode"}],"id2etag":{...},"id2error":{...}}`.
   Документация показывает только `id2etag` и `id2error`.
3. **`POST /task/move` возвращает объекты с дополнительными полями** —
   `isAllDay`, `isFloating`, `etimestamp`, `modifiedTime` поверх ожидаемых
   `id` и `etag`.
4. **У `Task` есть недокументированные поля** — `createdTime`, `modifiedTime`,
   `etimestamp`, `isFloating`.
5. **Краткие сведения о задаче в фокус-сессиях** (`OpenPomodoroTaskBrief`)
   содержат `projectName` и `tags`, которых нет в документации.

**Формат дат.** Все даты-время API использует `yyyy-MM-dd'T'HH:mm:ssZ`
с примером `2019-11-13T03:00:00+0000` (смещение без двоеточия; иногда
встречается `.000+0000`). Это **не RFC3339**, поэтому в спеке все такие поля
объявлены строками (`type: string`, без `format: date-time`) — иначе
`oapi-codegen` сгенерировал бы `time.Time`, который не парсит `+0000`.
CLI принимает `YYYY-MM-DD`, `"YYYY-MM-DD HH:MM"` и RFC3339 и нормализует их
в формат API (`internal/cli/timeutil.go`). Исключение — `createdTime`
и `modifiedTime` комментариев: это эпоха в миллисекундах (пункт 1).

## Архитектура

```text
api/
  frag-paths-task.yaml    # фрагмент спеки: preference + task-эндпоинты
  frag-paths-rest.yaml    # фрагмент: project/group/column/tag/focus/countdown/habit
  frag-schemas.yaml       # components/schemas (44 схемы)
  openapi.yaml            # итоговая спека (46 операций), собирается из фрагментов
  openapi.gen.go          # сгенерированный клиент + типы (~7350 строк)
cmd/ticktick/main.go      # entrypoint
internal/cli/             # каркас CLI и по файлу на группу команд
  root.go                 # корневая команда, persistent-флаги, Execute
  client.go               # клиент с Bearer-токеном, таймаутом, APIError
  config.go               # разрешение токена и base URL
  output.go               # Render/PrintJSON/Table/KV, текстовые рендеры
  timeutil.go             # нормализация времени, FmtEpochMs, FmtTime
  ptr.go                  # хелперы для указателей сгенерированных типов
  groups.go               # родительские команды групп
  task_crud.go            # task: get/create/update/complete/delete
  task_bulk.go            # task: complete-many/move/batch/assign/unassign
  task_query.go           # task: completed/filter/undone/search
  task_comments.go        # task: comments/comment-add/comment-delete/comments-batch
  project.go group.go column.go tag.go focus.go countdown.go habit.go preference.go
  *_test.go               # unit-тесты на httptest
internal/comments/        # батч-логика комментариев (воркеры 1–8)
docs/openapi.md           # снимок официальной документации (источник спеки)
scripts/merge-spec.sh     # сборка api/openapi.yaml из фрагментов
scripts/smoke.sh          # live smoke-тест (scratch-проекты, нужен токен)
PLAN.md                   # план и оркестрация реализации
Makefile                  # build/generate/test/vet/fmt/smoke/clean
```

Как это устроено:

- **Спека.** Машинно-читаемой OpenAPI-спеки у TickTick нет (документация —
  docsify-markdown). Спека составлена вручную по снимку `docs/openapi.md`
  и разбита на три фрагмента; `scripts/merge-spec.sh` склеивает их
  (`frag-paths-task.yaml` → `frag-paths-rest.yaml` → `components` →
  `frag-schemas.yaml`) в `api/openapi.yaml`. Ответы 4xx/5xx намеренно не
  описываются: фокус на бизнес-сущностях и успешных ответах.
- **Генерация.** `make generate` запускает `oapi-codegen` v2.4.1
  (`types,client`) и перезаписывает `api/openapi.gen.go`. Пакет `api`
  используется и CLI, и тестами.
- **Регистрация команд.** Каждый файл группы сам добавляет свои листовые
  команды через `func init()` в родительскую команду из `groups.go` — файлы не
  редактируют общий список, поэтому группы разрабатывались параллельно без
  конфликтов.
- **Клиент и ошибки.** `internal/cli/client.go` собирает
  `api.ClientWithResponses` с `Authorization: Bearer <token>`, `http.Client`
  с таймаутом и превращает не-2xx ответ в `APIError` (статус + тело).
- **Вывод.** `Render(raw, textFn)` — единая точка контракта: при `--json`
  печатает сырое тело ответа с отступом 2, иначе вызывает текстовый рендер.
  `PrintJSON` используется там, где результат собирается клиентом
  (батч комментариев).
- **Указатели.** Сгенерированные типы используют указатели для optional-полей;
  `ptr.go` даёт `Ptr`, `Str`, `I32`, `I64`, `F64`, `BoolVal`, `Strs`, чтобы
  не загромождать команды.
- **Время.** `timeutil.go` нормализует пользовательский ввод в формат API,
  форматирует ответы для человека (локальная зона, минуты) и отдельно
  обрабатывает эпоху в миллисекундах у комментариев.

## Разработка

| Цель | Что делает |
|---|---|
| `make build` | собирает `./ticktick` из `./cmd/ticktick` |
| `make generate` | перегенерирует `api/openapi.gen.go` (нужен `oapi-codegen` v2.4.1) |
| `make test` | `go test ./...` (110 тестов на `httptest`) |
| `make vet` | `go vet ./...` |
| `make fmt` | `go fmt ./...` |
| `make smoke` | собирает бинарь и запускает `scripts/smoke.sh` против живого API |
| `make clean` | удаляет бинарь |

Гонки и тайминги:

```bash
go test -race ./...
go vet ./...
```

Состояние проекта: `go build ./...`, `go vet ./...` — чисто; 110 тестов
проходят, включая прогон под `-race`; live smoke пройден.

### Smoke-тест

`scripts/smoke.sh` работает с **реальным** API и требует токен
(`TICKTICK_TOKEN` или `~/.config/ticktick/token`) и собранный `./ticktick`.
Безопасность: скрипт создаёт scratch-проекты `tt-cli-smoke-*`, меняет только
задачи и комментарии внутри них и удаляет проекты в конце (в том числе через
`trap` при падении). Вне scratch-проектов данные не изменяются.

Что проверяется: read-only списки (preference, projects, tags, groups, habits,
countdowns, focus, sections), создание проекта и задач, CRUD задачи,
комментарии и `comments-batch` с проверкой карты, search/filter/undone,
`project data`/`members`, assign/unassign, `task batch` (add + update),
`task move`, полный жизненный цикл focus-сессии, `complete-many`/`complete`/
`completed`, удаление комментария и задачи, а также наличие блока
`Example API response` в `--help`.

```bash
export TICKTICK_TOKEN="..."
make smoke
```

### Изменение спеки

1. Правьте нужный фрагмент в `api/frag-*.yaml` (не `api/openapi.yaml` —
   он собирается скриптом).
2. `bash scripts/merge-spec.sh` — пересобрать `api/openapi.yaml`.
3. `make generate` — обновить клиент и типы.
4. `make build` — поправить команды, если изменились имена полей.
5. `make vet test` — и прогнать smoke, если менялись запросы к API.

## Процесс реализации

Проект собран по плану из `PLAN.md`: работа разбита на небольшие чанки, каждый
выполнял субагент **DeepSeek V4.1 Flash**, а ведущий агент ревьюил диффы,
собирал, тестировал и мержил.

- **Git-ветки `chunk/*`** хранят историю по чанкам: `chunk/task-crud`,
  `chunk/task-bulk`, `chunk/task-query`, `chunk/project`, `chunk/comments`,
  `chunk/misc1`, `chunk/misc2`, `chunk/habit`. Каждый чанк — отдельный коммит
  (или merge-коммит) в `main`.
- **Фиксация контрактов до старта.** Имена схем, `operationId`, набор команд,
  формат вывода и разрешение токена были зафиксированы в `PLAN.md` заранее —
  поэтому параллельные агенты не расходились в названиях, а регистрация команд
  через `init()` позволила им не редактировать общие файлы.
- **Спека из снимка документации.** `docs/openapi.md` — снятая копия
  официальной документации; из неё вручную составлены фрагменты спеки, затем
  клиент сгенерирован `oapi-codegen`.
- **Ревью и приёмка.** После каждого чанка: `go build`, `go vet`, `go test`,
  затем live smoke с реальным API; по итогам smoke спека и типы были
  скорректированы под живое поведение API (см. «Расхождения»).

## Ограничения

- **Только персональный API-токен.** CLI не реализует OAuth-флоу (client
  id/secret, refresh token): токен создаётся вручную в веб-интерфейсе TickTick
  и передаётся через флаг, env или файл.
- **В API нет удаления привычек.** Эндпоинта `DELETE` для habit не существует,
  поэтому в CLI есть `habit create`/`update`/`checkin`, но нет `habit delete`.
- **Нет серверного батча комментариев.** `comments-batch` — клиентская
  реализация: N отдельных запросов, N = число задач; выигрыш только за счёт
  параллельности (1–8 воркеров), а не за счёт одного вызова.
- **Rate limits не документированы.** Официальных лимитов частоты запросов нет;
  при превышении API отвечает ошибкой, которая печатается как `API error:
  HTTP <код>: <тело>` с кодом возврата 1. Для больших батчей снижайте
  `--workers`.
- **Потолки выборок заданы API:** `complete-many` — ≤50 id; `task batch` — ≤50
  add и ≤50 update; `completed`/`filter` — ≤200 результатов; `undone` — диапазон
  ≤14 дней; `focus list` — сервер сам обрезает диапазон до 30 дней; `search`
  с пустыми ключевыми словами возвращает пустой массив.
- **Ошибки 4xx/5xx в спеке не описаны** — спека сфокусирована на
  бизнес-сущностях и успешных ответах, поэтому сгенерированные типы не содержат
  моделей ошибок.
- **Пагинация только у `project list`** (`--limit`/`--offset`); у остальных
  списочных методов API курсора нет.
