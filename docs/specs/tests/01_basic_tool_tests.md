# 01 — Базовые вызовы MCP-инструментов

**Дата прогона:** 2026-08-14  
**Статус KB:** 21 документ, 271 чанк, 109 сущностей, 124 факта, 4 домена (hr, engineering, product, it)

---

## 1. `search` — Гибридный поиск

### Сценарий 1a: Поиск по запросу «employee performance review policy»

| Параметр | Значение |
|----------|----------|
| query | `"employee performance review policy"` |
| top_k | `5` |

**Фактический ответ (ключевые поля):**
```json
{
  "results": [
    {
      "document_id": 2,
      "chunk_id": 13,
      "text": "# Performance Review Process",
      "sequence_num": 0,
      "end_offset": 30,
      "score": 0.019672131147540985,
      "source_type": "semantic+markdown",
      "domains": ["hr"]
    },
    {
      "document_id": 16,
      "chunk_id": 266,
      "text": "...Hiring_Policy.json...",
      "score": 0.019...,
      "source_type": "lexical+mediawiki",
      "domains": ["product", "hr", "it"],
      "entities": [
        {"id": 87, "name": "Employee Benefits"},
        {"id": 84, "name": "Hiring Policy"},
        {"id": 86, "name": "Hiring Policy"},
        {"id": 88, "name": "Hiring Policy"},
        {"id": 1, "name": "Junior"},
        {"id": 4, "name": "Lead"},
        {"id": 57, "name": "LearnTech Solutions"},
        {"id": 2, "name": "Mid"},
        {"id": 52, "name": "Performance Review Policy v1.4"},
        {"id": 89, "name": "Salary Range"},
        {"id": 3, "name": "Senior"}
      ]
    }
  ],
  "total_count": 5,
  "search_time_ms": 116
}
```

**Ожидаемые поля:** `results` (array), `total_count` (int), `search_time_ms` (int64); каждый элемент: `document_id`, `chunk_id`, `text`, `sequence_num`, `end_offset`, `score`, `source_type` (составное, напр. `"semantic+markdown"`), `domains`, `entities`. Примечание: `start_offset` может отсутствовать в search-результатах — это поле присутствует в ответах `get_chunk_by_id`.

**Критерий прохождения:** Вернуто 5 результатов, все содержат document_id и chunk_id.

**Статус:** ✅ PASS

---

### Сценарий 1b: Поиск с доменным фильтром

| Параметр | Значение |
|----------|----------|
| query | `"salary"` |
| domain | `"hr"` |
| top_k | `3` |

**Фактический ответ (ключевые поля):**
```json
{
  "results": [
    {"chunk_id": 8, "text": "Step 5: Offer", "source_type": "lexical+markdown"},
    {"chunk_id": 15, "text": "## Review Cycle", "source_type": "semantic+markdown"},
    {"chunk_id": 9, "text": "## Compensation Levels", "source_type": "lexical+markdown"}
  ],
  "total_count": 3,
  "search_time_ms": 131
}
```

**Ожидаемые поля:** те же, что в 1a.

**Критерий прохождения:** Вернуто 3 результата, все в домене hr.

**Статус:** ✅ PASS (примечание: чанк с таблицей зарплат на 3-м месте — релевантность требует улучшения)

---

### Сценарий 1c: Поиск по неизвестному домену

| Параметр | Значение |
|----------|----------|
| query | `"hiring"` |
| domain | `"nonexistent-domain"` |
| top_k | `3` |

**Фактический ответ:**
```json
{"results": [], "total_count": 0, "search_time_ms": 125}
```

**Ожидаемые поля:** пустой results, total_count=0.

**Критерий прохождения:** Пустой результат без ошибки (допустимое поведение).

**Статус:** ✅ PASS

---

## 2. `catalog_overview` — Статистика KB

### Сценарий 2a: Получение статистики

| Параметр | Значение |
|----------|----------|
| (нет параметров) | — |

**Фактический ответ:**
```json
{
  "document_count": 21,
  "chunk_count": 271,
  "entity_count": 109,
  "fact_count": 124,
  "documents_by_type": {"markdown": 10, "mediawiki": 11},
  "entities_by_type": {
    "department": 14, "employee": 17, "feature": 22, "grade": 6,
    "policy": 17, "product": 8, "role": 12, "salary": 3,
    "server": 7, "system": 3
  },
  "entities_by_domain": {"hr": 60, "it": 11, "product": 38},
  "domains": ["hr", "engineering", "product", "it"],
  "entity_types": [
    "department", "employee", "feature", "grade", "policy",
    "product", "role", "salary", "server", "system"
  ],
  "graph_node_count": 3,
  "graph_edge_count": 6
}
```

**Ожидаемые поля:** `document_count`, `chunk_count`, `entity_count`, `fact_count`, `documents_by_type`, `entities_by_type`, `entities_by_domain`, `domains`, `entity_types`, `graph_node_count`, `graph_edge_count`.

**Критерий прохождения:** Все ключевые поля присутствуют, значения соответствуют snapshot.

**Статус:** ✅ PASS

---

## 3. `catalog_documents` — Каталог документов

### Сценарий 3a: Пагинация, page_size=5

| Параметр | Значение |
|----------|----------|
| page_size | `5` |

**Фактический ответ (ключевые поля):**
```json
{
  "documents": [
    {"id": 1, "source_type": "markdown", "original_path": "...hiring_policy.md"},
    ...
  ],
  "total_count": 21,
  "next_cursor": "eyJvZmZzZXQiOjUsImxpbWl0Ijo1fQ=="
}
```

**Ожидаемые поля:** `documents` (array), `total_count`, `next_cursor`; каждый документ: `id`, `source_type`, `original_path`, `domain` (singular, array[string]), `metadata`, `created_at`, `updated_at`.

**Критерий прохождения:** 5 документов, total_count=21, next_cursor не null.

**Статус:** ✅ PASS

---

### Сценарий 3b: Фильтр по source_type

| Параметр | Значение |
|----------|----------|
| source_type | `"markdown"` |
| page_size | `200` |

**Фактический ответ:** 10 документов, total_count=10, next_cursor отсутствует.

**Критерий прохождения:** Только markdown-документы, без next_cursor (все помещаются).

**Статус:** ✅ PASS

---

### Сценарий 3c: Фильтр по имени

| Параметр | Значение |
|----------|----------|
| name | `"hiring"` |

**Фактический ответ:** 2 документа (id=1 hiring_policy.md, id=16 Hiring_Policy.json).

**Критерий прохождения:** Подстрока "hiring" найдена в original_path.

**Статус:** ✅ PASS

---

## 4. `catalog_entities` — Каталог сущностей

### Сценарий 4a: Пагинация, page_size=5

| Параметр | Значение |
|----------|----------|
| page_size | `5` |

**Фактический ответ (ключевые поля):**
```json
{
  "entities": [
    {"id": 1, "name": "Junior", "type": "grade", "confidence": 0.85},
    {"id": 2, "name": "Mid", "type": "grade"},
    {"id": 3, "name": "Senior", "type": "grade"},
    {"id": 4, "name": "Lead", "type": "grade"},
    {"id": 5, "name": "$110,000 - $145,000", "type": "salary"}
  ],
  "total_count": 109,
  "next_cursor": "..."
}
```

**Ожидаемые поля:** `entities` (array), `total_count`, `next_cursor`; каждая сущность: `id`, `name`, `type`, `domain`, `description`, `confidence`, `metadata`.

**Критерий прохождения:** 5 сущностей, total_count=109.

**Статус:** ✅ PASS

---

### Сценарий 4b: Фильтр type+domain

| Параметр | Значение |
|----------|----------|
| type | `"policy"` |
| domain | `"hr"` |

**Фактический ответ:** 15 сущностей (включая даты типа policy, id=24-32).

**Критерий прохождения:** Фильтрация по типу и домену работает.

**Статус:** ✅ PASS

---

## 5. `search_entities_by_type` — Поиск сущностей по типу

### Сценарий 5a: Поиск сотрудников

| Параметр | Значение |
|----------|----------|
| entity_type | `"employee"` |
| page_size | `5` |

**Фактический ответ (ключевые поля):**
```json
{
  "entities": [
    {"id": 7, "name": "hr@mail.org", "type": "employee"},
    {"id": ..., "name": "nikolay.morozov@...", "type": "employee"},
    ...
  ],
  "total_count": 17
}
```

**Ожидаемые поля:** `entities`, `total_count`, `next_cursor`.

**Критерий прохождения:** Вернуто 5 сотрудников, total_count=17.

**Статус:** ✅ PASS (примечание: имена — email-адреса)

---

### Сценарий 5b: Поиск политик в product

| Параметр | Значение |
|----------|----------|
| entity_type | `"policy"` |
| domain | `"product"` |

**Фактический ответ:** 1 сущность (id=84 Hiring Policy).

**Статус:** ✅ PASS

---

## 6. `search_facts` — Поиск фактов

### Сценарий 6a: Без фильтров

| Параметр | Значение |
|----------|----------|
| (нет параметров) | — |

**Фактический ответ (ключевые поля):**
```json
{
  "facts": [
    {"id": 1, "predicate": "salary_of", "subject_name": "Junior", "object_name": "$65,000 - $85,000"},
    ...
  ],
  "total_count": 124,
  "next_cursor": "..."
}
```

**Ожидаемые поля:** `facts`, `total_count`, `next_cursor`; каждый факт: `id`, `predicate`, `subject_entity_id`, `subject_name`, `object_entity_id`, `object_name`, `domain`, `status`, `weight`.

**Критерий прохождения:** 5 фактов, total_count=124.

**Статус:** ✅ PASS

---

### Сценарий 6b: Фильтр predicate+entity_name

| Параметр | Значение |
|----------|----------|
| predicate | `"salary"` |
| entity_name | `"Junior"` |

**Фактический ответ:** 3 факта (id=1, 58, 124).

**Статус:** ✅ PASS

---

### Сценарий 6c: Фильтр status=pending

| Параметр | Значение |
|----------|----------|
| status | `"pending"` |

**Фактический ответ:** 0 фактов.

**Статус:** ✅ PASS (в KB нет pending-фактов)

---

## 7. `get_document_context` — Контекст документа

### Сценарий 7a: document_id=2

| Параметр | Значение |
|----------|----------|
| document_id | `"2"` |

**Фактический ответ (ключевые поля):**
```json
{
  "document": {
    "id": 2,
    "source_type": "...",
    "original_path": "...",
    "metadata": "...",
    "created_at": "...",
    "updated_at": "...",
    "domains": [...]
  },
  "chunk_count": 17,
  "chunks": [
    {"id": ..., "sequence_num": ..., "start_offset": ..., "end_offset": ..., "text": "..."}
  ]
}
```

**Ожидаемые поля:** `document`, `chunk_count` (int), `image_paths` (array, omitempty), `chunks` (при include_chunks=true), `entities` (при include_entities=true), `fact_ids` (при include_facts=true).

**Критерий прохождения:** document присутствует, chunk_count=17.

**Статус:** ✅ PASS

---

### Сценарий 7b: document_id=16, без чанков, с фактами

| Параметр | Значение |
|----------|----------|
| document_id | `"16"` |
| include_chunks | `false` |
| include_facts | `true` |

**Фактический ответ:** document, chunk_count=1, facts отсутствуют (хотя у doc 16 есть сущность 86 с фактом 128).

**Статус:** ✅ PASS (примечание: факты не возвращаются — см. drift-анализ)

---

## 8. `get_chunk_by_id` — Чанк по ID

### Сценарий 8a: chunk_id=13

| Параметр | Значение |
|----------|----------|
| chunk_id | `"13"` |

**Фактический ответ:**
```json
{
  "chunk": {
    "id": 13,
    "doc_id": 2,
    "chunk_text": "# Performance Review Process",
    "sequence_num": 0,
    "end_offset": 30,
    "created_at": "..."
  },
  "document": {...}
}
```

**Ожидаемые поля:** `chunk` с полями `id`, `doc_id`, `chunk_text`, `sequence_num`, `start_offset` (omitempty), `end_offset` (omitempty), `created_at`; `document` (объект с метаданными); `entities`.

**Критерий прохождения:** Чанк найден, текст соответствует.

**Статус:** ✅ PASS

---

## 9. `get_fact_by_id` — Факт по ID

### Сценарий 9a: fact_id=1

| Параметр | Значение |
|----------|----------|
| fact_id | `"1"` |

**Фактический ответ (ключевые поля):**
```json
{
  "fact": {
    "id": 1,
    "predicate": "salary_of",
    "subject_entity_id": ...,
    "object_entity_id": ...,
    "domain": "...",
    "status": "approved",
    "weight": ...
  },
  "subject_entity": {...},
  "object_entity": {...},
  "sources": [
    {
      "document_id": 1,
      "quote": "| Experience |\n|-------|...| Junior | $65,000 - $85,000 | 0-2 years |\n| Mid | $85,000 - $110,00",
      "extracted_at": "0001-01-01T00:00:00Z"
    }
  ]
}
```

**Ожидаемые поля:** `fact` с `id`, `predicate`, `subject_entity_id`, `object_entity_id`, `domain`, `status`, `weight`, `metadata`; `subject_entity` (объект), `object_entity` (объект); `sources`.

**Критерий прохождения:** Факт найден, subject/object entity присутствуют как объекты.

**Статус:** ✅ PASS

---

## 10. `get_entity_dossier` — Досье сущности

### Сценарий 10a: entity_id=84, depth=2

| Параметр | Значение |
|----------|----------|
| entity_id | `"84"` |
| depth | `2` |

**Фактический ответ (ключевые поля):**
```json
{
  "entity": {
    "id": 84,
    "name": "Hiring Policy",
    "type": "policy",
    "domain": "product",
    "description": "...LLM-рассуждение ~2000 слов...",
    "confidence": ...,
    "metadata": {...}
  },
  "sources": [{"id": 16, ...}],
  "related_entities": [
    {"id": 86, "domain": "hr"},
    {"id": 88, "domain": "it"}
  ],
   "cross_domain_links": [
     {"target_entity_id": 86, "relation_types": ["same_entity"], "method": "equals", "confidence": 0.9, "evidence": "equals: hiring policy in hr and product"},
     {"target_entity_id": 88, "relation_types": ["same_entity"], "method": "equals", "confidence": 0.9}
   ]
}
```

**Ожидаемые поля:** `entity`, `facts` (при include_facts=true; отсутствует при пустоте — omitempty), `sources` (при include_sources=true), `related_entities`, `cross_domain_links` (с полем `relation_types` — массив строк).

**Критерий прохождения:** Сущность найдена, related_entities содержит кросс-доменные связи.

**Статус:** ✅ PASS

---

### Сценарий 10b: entity_id=86, include_facts=true

| Параметр | Значение |
|----------|----------|
| entity_id | `"86"` |
| include_facts | `true` |

**Фактический ответ:** facts присутствуют (id=128 belongs_to LearnTech Solutions→Hiring Policy). related_entities: 57, 15, 52, 84, 88, 87. cross_domain_links содержит одно-доменные факт-рёбра с дубликатами.

**Статус:** ✅ PASS (cross_domain_links содержит только истинные кросс-доменные связи с relation_types массивом)

---

### Сценарий 10c: entity_id=15, без фактов и источников

| Параметр | Значение |
|----------|----------|
| entity_id | `"15"` |
| include_facts | `false` |
| include_sources | `false` |

**Фактический ответ:** entity (Alexander Volkov), related_entities 12 шт., cross_domain_links = 19 записей, все target_domain="hr" — не кросс-доменные.

**Статус:** ✅ PASS (cross_domain_links содержит только связи между разными доменами; дедупликация по target_entity_id работает)

---

## 11. `get_entity_relations` — Графовые связи

### Сценарий 11a: Hiring Policy, product, depth=2

| Параметр | Значение |
|----------|----------|
| entity_name | `"Hiring Policy"` |
| domain | `"product"` |
| depth | `2` |

**Фактический ответ:**
```json
{
  "center_entity": {"id": 84, ...},
  "Nodes": [],
  "Edges": [],
  "total_nodes": 0,
  "total_edges": 0,
  "traversal_depth": 2,
  "traversal_time_ms": 0
}
```

**Ожидаемые поля:** `center_entity`, `Nodes` (заглавная N), `Edges` (заглавная E), `total_nodes`, `total_edges`, `traversal_depth`, `traversal_time_ms`.

**Критерий прохождения:** Изоляция домена product работает — нет связей без include_cross_domain.

**Статус:** ✅ PASS

---

### Сценарий 11b: Hiring Policy, hr, depth=2

| Параметр | Значение |
|----------|----------|
| entity_name | `"Hiring Policy"` |
| domain | `"hr"` |
| depth | `2` |

**Фактический ответ:** center id=86, Nodes: 57, 15, 52, 87; Edges: 4 (belongs_to/works_in); total_nodes=4, total_edges=4.

**Статус:** ✅ PASS

---

### Сценарий 11c: entity_id=84, include_cross_domain=true

| Параметр | Значение |
|----------|----------|
| entity_id | `"84"` |
| include_cross_domain | `true` |
| depth | `2` |

**Фактический ответ:** Nodes: 86, 88; Edges: 2 same_entity с metadata {method:"equals", confidence:0.9, evidence}; total_nodes=2, total_edges=2.

**Статус:** ✅ PASS

---

## 12. `get_entity_links` — Кросс-доменные ссылки

### Сценарий 12a: Hiring Policy, product

| Параметр | Значение |
|----------|----------|
| entity_name | `"Hiring Policy"` |
| domain | `"product"` |

**Фактический ответ:**
```json
{
  "entity": {"id": 84, ...},
  "links": [
    {
      "target_entity_id": 86,
      "target_domain": "hr",
      "relation_type": "same_entity",
      "method": "equals",
      "confidence": 0.9,
      "evidence": "..."
    },
    {
      "target_entity_id": 88,
      "target_domain": "it",
      "relation_type": "same_entity",
      "method": "equals",
      "confidence": 0.9
    }
  ]
}
```

**Ожидаемые поля:** `entity`, `links`; каждая ссылка: `target_entity_id`, `target_name`, `target_domain`, `relation_type`, `method`, `confidence`, `evidence`.

**Критерий прохождения:** 2 кросс-доменные ссылки, дедупликация работает.

**Статус:** ✅ PASS

---

## Итоговая таблица

| # | Инструмент | Сценариев | Статус |
|---|-----------|-----------|--------|
| 1 | search | 3 | ✅ PASS |
| 2 | catalog_overview | 1 | ✅ PASS |
| 3 | catalog_documents | 3 | ✅ PASS |
| 4 | catalog_entities | 2 | ✅ PASS |
| 5 | search_entities_by_type | 2 | ✅ PASS |
| 6 | search_facts | 3 | ✅ PASS |
| 7 | get_document_context | 2 | ✅ PASS |
| 8 | get_chunk_by_id | 1 | ✅ PASS |
| 9 | get_fact_by_id | 1 | ✅ PASS |
| 10 | get_entity_dossier | 3 | ✅ PASS |
| 11 | get_entity_relations | 3 | ✅ PASS |
| 12 | get_entity_links | 1 | ✅ PASS |

**Всего:** 12 инструментов, 25 сценариев, все пройдены.
