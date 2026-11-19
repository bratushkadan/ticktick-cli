# План: TickTick Open API — Go-клиент + CLI

## Цель
Заменить ограниченный/медленный/дорогой MCP-сервер TickTick собственным Go-клиентом,
сгенерированным из OpenAPI-спеки, и CLI-приложением `ticktick`, пригодным для скриптов
и AI-скиллов (экономия контекста, производительность).

## Источник истины
- Официальная документация: https://developer.ticktick.com/docs#/openapi
  (docsify-markdown, машинно-читаемой OpenAPI-спеки у TickTick НЕТ).
- Копия документации: `docs/openapi.md` (снята 2026-11-20).
- Спеку `api/openapi.yaml` составляем сами строго по документации, затем генерируем
  клиент через `oapi-codegen` (types + client).
- Base URL: `https://api.ticktick.com`. Авторизация: `Authorization: Bearer <token>`
  (персональный API-токен проверен, работает).

## Принципы (требования пользователя)
1. Фокус на ВСЕХ бизнес-сущностях API, а не на описании 4xx-кодов.
2. В `--help` КАЖДОЙ команды — пример ответа API (JSON из документации).
3. Вывод по умолчанию — текстовый (человекочитаемый, компактный).
4. `--json` доступен в КАЖДОЙ команде (persistent flag корня) — машинный вывод,
   сырой JSON ответа API, для скриптов и скиллов.
5. `GetTasksComments`: список id задач → `map[taskId][]Comment`. В API батч-эндпоинта
   нет → клиентская реализация: 1–8 воркеров (default 4), concurrent-запросы
   `GET /open/v1/project/{projectId}/task/{taskId}/comments`.
   ВАЖНО: API требует projectId. Поэтому CLI принимает либо
   `--project P --ids id1,id2`, либо `--tasks P1:T1,P2:T2` (смешанные проекты).
   JSON-вывод: объект `{"<taskId>": [comments...]}`; ошибки по задачам — в stderr,
   код возврата 1 при частичном сбое (карта остаётся чистой map[id][]).
6. Git-версионирование: коммит на каждый завершённый чанк; ветки — при сильных
   сомнениях в дизайне. Финальный результат отражён в README.md.
7. Оркестрация: малые чанки субагентам `opencode-go/deepseek-v4.1-flash`
   (DeepSeek V4.1 Flash 1017); fallback — `opencode-go/qwen4-max`. Lead ревьюит
   каждый дифф, собирает и тестирует.

## Ключевые технические решения
- **Даты — строки.** API использует `yyyy-MM-dd'T'HH:mm:ssZ` (например
  `2019-11-13T03:00:00+0000`, встречается `.000+0000`) — НЕ RFC3339.
  В спеке все date-time поля = `type: string` с описанием формата (без
  `format: date-time`), чтобы oapi-codegen не генерировал `time.Time`,
  который не парсит `+0000`. CLI-хелпер `internal/cli/timeutil.go` нормализует
  пользовательский ввод (RFC3339, `YYYY-MM-DD`, `YYYY-MM-DD HH:MM`) в формат API.
- **Модуль** `ticktick-cli`, бинарь `ticktick` (cmd/ticktick/main.go).
- **CLI-фреймворк**: cobra. Зависимости минимальны: cobra + oapi-codegen/runtime.
- **Регистрация команд**: каждый файл команд сам добавляет себя в корень через
  `func init()` — параллельные агенты не редактируют общие файлы.
- **Клиент**: сгенерированный `api.Client` + `WithResponse`-варианты; обёртка
  `internal/cli/client.go` добавляет Bearer-токен, таймаут, понятные ошибки
  (статус + тело ответа). Токен: флаг `--token` > env `TICKTICK_TOKEN` >
  файл `~/.config/ticktick/token`. Токен НЕ коммитится в репозиторий.
- **Вывод**: `internal/cli/output.go` — `PrintJSON(v)` (indent 2, в stdout) и
  текстовые хелперы (таблицы/ключ-значение). Каждая команда реализует свой
  текстовый рендер; `--json` печатает сырой ответ API.
- **Схемы (имена фиксированы, чтобы чанки не расходились)**:
  Task, ChecklistItem, Project, ProjectData, Column, OpenFocusSummary,
  OpenPomodoroTaskBrief, OpenFocus, OpenHabit, OpenHabitSection,
  OpenHabitCheckinData, OpenHabitCheckin, OpenComment, OpenProjectGroup,
  OpenTag, OpenCountdown, Preference, ProjectMember, MoveTaskResult,
  TaskCreateRequest, TaskUpdateRequest, CompleteTasksRequest, TaskMove,
  TaskBatchRequest, TaskBatchResult, TaskAssignRequest, TaskUnassignRequest,
  CompletedTasksFilter, TaskFilter, UndoneTasksFilter, TaskSearchFilter,
  CommentCreateRequest, ProjectCreateRequest, ProjectUpdateRequest,
  ProjectGroupCreateRequest, ProjectGroupUpdateRequest, ColumnCreateRequest,
  ColumnUpdateRequest, TagCreateRequest, FocusCreateRequest,
  HabitCreateRequest, HabitUpdateRequest, HabitCheckinRequest.
- **operationId (фиксированы)**: GetUserPreferences, GetTask, CreateTask,
  UpdateTask, CompleteTask, CompleteTasks, DeleteTask, MoveTasks, BatchTasks,
  AssignTask, UnassignTask, ListCompletedTasks, FilterTasks, ListUndoneTasks,
  SearchTasks, GetTaskComments, AddTaskComment, DeleteTaskComment,
  ListProjects, GetProject, GetProjectData, GetProjectMembers, CreateProject,
  UpdateProject, DeleteProject, ListProjectGroups, CreateProjectGroup,
  UpdateProjectGroup, DeleteProjectGroup, ListColumns, CreateColumn,
  UpdateColumn, ListTags, CreateTag, GetFocus, ListFocuses, CreateFocus,
  DeleteFocus, ListCountdowns, GetHabit, ListHabitSections, ListHabits,
  CreateHabit, UpdateHabit, CheckinHabit, ListHabitCheckins.
- Ответы 4xx/5xx в спеке НЕ описываем детально (только default-ответ без схемы);
  фокус — 200-ответы и бизнес-сущности.

## Структура репозитория
```
api/openapi.yaml         — спека (составлена из docs/openapi.md)
api/openapi.gen.go       — сгенерированный клиент+типы (package api)
cmd/ticktick/main.go     — entrypoint
internal/cli/            — каркас и команды (по файлу на группу)
internal/comments/       — батч-логика GetTasksComments (воркеры 1–8)
docs/openapi.md          — копия официальной документации
scripts/smoke.sh         — live smoke-тест (токен из env)
PLAN.md, README.md, Makefile
```

## Чанки и оркестрация
| # | Чанк | Исполнитель | Зависимости |
|---|------|-------------|-------------|
| 0 | Репо, план, тулчейн (go1.23.4 есть, oapi-codegen ставится) | lead | — |
| 1a | openapi.yaml: components/schemas (все сущности) + paths Preference/Task | Flash A | 0 |
| 1b | openapi.yaml: paths Project/Group/Column/Tag/Focus/Countdown/Habit | Flash B | 0 (имена схем зафиксированы выше) |
| 2 | Слияние спеки, генерация клиента, компиляция | lead | 1a,1b |
| 3 | Каркас CLI: root.go, client.go, output.go, timeutil.go, main.go, Makefile | lead | 2 |
| 4a | task CRUD: get/create/update/complete/delete (+тесты) | Flash C1 | 3 |
| 4b | task bulk: complete-many/move/batch/assign/unassign (+тесты) | Flash C2 | 3 |
| 4c | task query: completed/filter/undone/search (+тесты) | Flash C3 | 3 |
| 4d | comments: list/add/delete + internal/comments batch (воркеры) + comments-batch (+тесты) | Flash C4 | 3 |
| 4e | project: list/get/data/members/create/update/delete (+тесты) | Flash C5 | 3 |
| 4f | group + column + tag (+тесты) | Flash C6 | 3 |
| 4g | focus + countdown + preference (+тесты) | Flash C7 | 3 |
| 4h | habit: list/get/sections/create/update/checkin/checkins (+тесты) | Flash C8 | 3 |
| 5 | Интеграция: build/vet/test, ревью диффов, фиксы | lead (+Flash/qwen при затыках) | 4a–4h |
| 6 | Live smoke-тест с реальным API (scratch-проект, комментарии, батч; только read-only вне scratch) | lead | 5 |
| 7 | README.md (RU): установка, все команды, примеры, батч, скриптинг | Flash D + ревью lead | 6 |
| 8 | Финальный коммит/тег, отчёт | lead | 7 |

## Критерии приёмки
- [ ] `go build ./...`, `go vet ./...`, `go test ./...` — зелёные.
- [ ] Все ~40 эндпоинтов документации покрыты командами CLI.
- [ ] `--json` работает в каждой команде; текстовый вывод по умолчанию.
- [ ] `--help` каждой команды содержит пример ответа API.
- [ ] `task comments-batch` возвращает map[id][]comments, воркеры 1–8,
      конкурентность подтверждена тестом.
- [ ] Live smoke: создание scratch-проекта/задачи/комментариев, батч-чтение,
      complete/delete, удаление scratch-проекта — успешно, данные пользователя
      вне scratch не изменяются.
- [ ] README отражает финальный результат; история Git осмысленная.

## Риски и план Б
- Flash «тупит» на чанке → дробим чанк ещё мельче или передаём qwen4-max.
- Расхождения спеки с реальным API → правим спеку по факту live-ответов
  (спека — наш артефакт, это легитимно), перегенерируем.
- Сомнения в дизайне → отдельная ветка Git, сравнение, выбор в README.