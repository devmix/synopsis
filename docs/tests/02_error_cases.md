# 02 — Негативные сценарии (Error Cases)

**Дата прогона:** 2026-08-14  
**Цель:** Проверка обработки ошибок, валидации аргументов и граничных условий.

---

## Таблица error-кейсов

| # | Вызов | Ожидаемая ошибка (docs/api.md) | Фактическая ошибка | Статус | Примечание |
|---|-------|-------------------------------|-------------------|--------|------------|
| 1 | `search` query="" | `"Error: 'query' argument is required and must not be empty"` | `"Error: 'query' argument is required and must not be empty"` | ✅ PASS | — |
| 2 | `search` top_k=0 | `"Error: 'top_k' must be between 1 and 100, got 0"` | `"Error: 'top_k' must be between 1 and 100, got 0"` | ✅ PASS | Валидация добавлена; явная ошибка при top_k < 1 |
| 3 | `search` top_k=101 | `"Error: 'top_k' must be between 1 and 100, got 101"` | `"Error: 'top_k' must be between 1 and 100, got 101"` | ✅ PASS | Валидация добавлена; явная ошибка при top_k > 100 |
| 4 | `catalog_documents` cursor="invalid-cursor!" | `"Error decoding cursor: ..."` | `"Error decoding cursor: decode cursor: illegal base64 data at input byte 7"` | ✅ PASS | — |
| 5 | `catalog_entities` cursor="invalid-cursor!" | `"Error decoding cursor: ..."` | `"Error decoding cursor: decode cursor: illegal base64 data at input byte 7"` | ✅ PASS | — |
| 6 | `search_facts` cursor="invalid-cursor!" | `"Error decoding cursor: ..."` | `"Error decoding cursor: decode cursor: illegal base64 data at input byte 7"` | ✅ PASS | — |
| 7 | `search_entities_by_type` entity_type="" | `"Error: 'entity_type' argument is required..."` | `"Error: 'entity_type' argument is required and must not be empty"` | ✅ PASS | — |
| 8 | `get_document_context` document_id="abc" | `"Error: 'document_id' must be an integer, got \"...\""` | `"Error: 'document_id' must be an integer, got \"abc\""` | ✅ PASS | — |
| 9 | `get_document_context` document_id="99999" | `"Document with ID N not found"` | `"Document with ID 99999 not found"` | ✅ PASS | — |
| 10 | `get_chunk_by_id` chunk_id="99999" | `"Chunk with ID N not found"` | `"Chunk with ID 99999 not found"` | ✅ PASS | — |
| 11 | `get_fact_by_id` fact_id="99999" | `"Fact with ID N not found"` | `"Fact with ID 99999 not found"` | ✅ PASS | — |
| 12 | `get_entity_dossier` (без аргументов) | `"Error: either 'entity_id' or 'entity_name' must be provided"` | `"Error: either 'entity_id' or 'entity_name' must be provided"` | ✅ PASS | — |
| 13 | `get_entity_dossier` entity_id="84" + entity_name="Hiring Policy" | `"Error: provide exactly one of 'entity_id' or 'entity_name', not both"` | `"Error: provide either 'entity_id' or 'entity_name', not both"` | ⚠️ FAIL | Формулировка отличается от документации ("provide either" вместо "provide exactly one") |
| 14 | `get_entity_dossier` entity_id="abc" | `"Error: 'entity_id' must be an integer, got \"...\""` | `"Error: 'entity_id' must be an integer, got \"abc\""` | ✅ PASS | — |
| 15 | `get_entity_dossier` entity_id="99999" | `"Entity ID N not found"` | `"Entity ID 99999 not found"` | ✅ PASS | — |
| 16 | `get_entity_dossier` entity_name="Hiring Policy" (без domain) | Error listing candidate IDs for disambiguation | `"Multiple entities match \"Hiring Policy\". Please specify one (optionally with a domain): - Hiring Policy (id=84, type=policy) [product] - Hiring Policy (id=86, type=policy) [hr] - Hiring Policy (id=88, type=policy) [it]"` | ✅ PASS | Дисамбигуация работает корректно |
| 17 | `get_entity_relations` entity_id="99999" | `"Entity ID N not found in graph"` | `"Entity ID 99999 not found in graph"` | ✅ PASS | — |
| 18 | `get_entity_links` entity_id="99999" | `"Entity ID N not found"` | `"Entity ID 99999 not found"` | ✅ PASS | — |

---

## Сводка

| Категория | Всего | PASS | FAIL | Примечание |
|-----------|-------|------|------|------------|
| Валидация обязательных аргументов | 3 | 3 | 0 | query, entity_type, entity_id/entity_name — все работают |
| Валидация типов (integer) | 2 | 2 | 0 | document_id="abc", entity_id="abc" — корректно |
| Несуществующие ID | 5 | 5 | 0 | Все возвращают "not found" с конкретным ID |
| Невалидный cursor | 3 | 3 | 0 | Единое сообщение об ошибке декодирования base64 |
| Конфликт entity_id+entity_name | 1 | 0 | 1 | Формулировка отличается от docs/api.md |
| Дисамбигуация по имени | 1 | 1 | 0 | Работает, список кандидатов с domain-подсказкой |
| Граничные значения top_k | 2 | 2 | 0 | **top_k=0 и top_k=101 отклоняются** — явная ошибка с сообщением |

---

## Критические замечания

### 1. Валидация `top_k` исправлена (кейсы #2, #3)

Документация (`docs/api.md`) указывает constraint: **1–100**. Фактическое поведение после исправления:
- `top_k=0` → явная ошибка `"Error: 'top_k' must be between 1 and 100, got 0"`.
- `top_k=101` → явная ошибка `"Error: 'top_k' must be between 1 and 100, got 101"`.

**Статус:** ✅ Исправлено. Валидация добавлена в `internal/mcp/handlers/search.go`.

### 2. Несоответствие формулировки ошибки (кейс #13)

Документация: `"Error: provide exactly one of 'entity_id' or 'entity_name', not both"`  
Факт: `"Error: provide either 'entity_id' or 'entity_name', not both"`

**Рекомендация:** Синхронизировать формулировки.

### 3. Поиск по неизвестному домену — пустой результат без предупреждения

`search(query="hiring", domain="nonexistent-domain")` → `{"results":[],"total_count":0}`  
Нет указания на то, что домен не существует. Клиент может интерпретировать это как «нет совпадений в домене».

**Рекомендация:** Добавить предупреждение или отдельный код ошибки для неизвестного домена.
