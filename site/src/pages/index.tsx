import {useEffect, useRef, useState} from 'react';
import Link from '@docusaurus/Link';
import Layout from '@theme/Layout';
import type {ReactNode} from 'react';

import styles from './index.module.css';

const GITHUB_URL = 'https://github.com/devmix/synopsis';

/* ============================= data ============================= */

type Tone = 'cmd' | 'ok' | 'dim' | 'hl';

const TONE_CLASS: Record<Tone, string> = {
  cmd: styles.tCmd,
  ok: styles.tOk,
  dim: styles.tDim,
  hl: styles.tHl,
};

interface TermLine {
  readonly text: string;
  readonly tone: Tone;
}

const TERMINAL_LINES: readonly TermLine[] = [
  {text: '$ make build', tone: 'cmd'},
  {text: '✓ bin/synopsis — single binary · sqlite fts5+vec', tone: 'ok'},
  {text: '$ make sync', tone: 'cmd'},
  {text: '✓ 1,284 docs · 12,902 chunks embedded (onnx)', tone: 'ok'},
  {text: '✓ 3,411 entities · 9,027 facts · graph loaded', tone: 'ok'},
  {text: '$ make run', tone: 'cmd'},
  {text: '✓ mcp server → :8080/sse', tone: 'ok'},
  {text: '$ search {"query":"vacation policy","domain":"hr"}', tone: 'cmd'},
  {text: '✓ 10 chunks · bm25+vector · rrf k=20', tone: 'hl'},
];

const TICKER_ITEMS: readonly string[] = [
  'Go 1.25',
  'MCP',
  'SQLite',
  'FTS5',
  'sqlite-vec',
  'ONNX Runtime',
  'RRF k=20',
  'NER',
  'Knowledge Graph',
  'HTTP / SSE',
  'Zig cross-compile',
  'Single binary',
];

interface PipelineStage {
  readonly num: string;
  readonly title: string;
  readonly desc: string;
  readonly meta: string;
  readonly chips: readonly string[];
  readonly final?: boolean;
}

const PIPELINE_STAGES: readonly PipelineStage[] = [
  {
    num: '01',
    title: 'Clean data',
    desc: 'Documents arrive normalized and tagged with domains. Synopsis does not clean or deduplicate — messy data is a stage you run upstream.',
    meta: '.md · .json · .html',
    chips: ['Markdown', 'JSON', 'HTML'],
  },
  {
    num: '02',
    title: 'Unified format',
    desc: 'A format-specific parser turns every file into the same structured document — text, metadata, and source tags preserved.',
    meta: 'sources declared in ontology xml',
    chips: ['Parser', 'Metadata', 'Source tags'],
  },
  {
    num: '03',
    title: 'Processing',
    desc: 'sync runs parse → chunk → embed → NER → entities → facts into a knowledge graph plus a hybrid search index, then links entities across domains.',
    meta: 'make sync · one sqlite file',
    chips: ['Chunking', 'Embeddings', 'NER', 'Knowledge graph', 'Hybrid index'],
  },
  {
    num: '04',
    title: 'MCP access',
    desc: 'serve exposes 12 tools over HTTP/SSE. Agents retrieve chunks, entities, facts, and graph neighbors — every answer with full provenance.',
    meta: 'GET /sse · POST /message',
    chips: ['HTTP/SSE', '12 tools', 'Provenance'],
    final: true,
  },
];

interface Feature {
  readonly num: string;
  readonly icon: ReactNode;
  readonly name: string;
  readonly desc: string;
  readonly tags: readonly string[];
}

const iconProps = {
  viewBox: '0 0 24 24',
  fill: 'none',
  stroke: 'currentColor',
  strokeWidth: 1.7,
  strokeLinecap: 'round',
  strokeLinejoin: 'round',
  'aria-hidden': true,
} as const;

const FEATURES: readonly Feature[] = [
  {
    num: '/01',
    icon: (
      <svg {...iconProps}>
        <circle cx="11" cy="11" r="7" />
        <path d="m21 21-4.3-4.3" />
        <path d="M8 11h6" />
      </svg>
    ),
    name: 'Hybrid Search',
    desc: 'Lexical (BM25) and semantic (vector) retrieval run in parallel, fused with Reciprocal Rank Fusion (k=20) and reranked with authority and freshness boosts.',
    tags: ['FTS5/BM25', 'sqlite-vec', 'RRF k=20'],
  },
  {
    num: '/02',
    icon: (
      <svg {...iconProps}>
        <circle cx="5" cy="6" r="2.5" />
        <circle cx="19" cy="6" r="2.5" />
        <circle cx="12" cy="18" r="2.5" />
        <path d="M7.5 6h9" />
        <path d="M6.2 8.2 10.8 15.9" />
        <path d="M17.8 8.2 13.2 15.9" />
      </svg>
    ),
    name: 'Knowledge Graph',
    desc: 'Entities, facts, and relations load into memory at startup — O(1) lookups, BFS traversal, and a complete dossier per entity, facts and quotes included.',
    tags: ['in-memory', 'BFS', 'dossiers'],
  },
  {
    num: '/03',
    icon: (
      <svg {...iconProps}>
        <rect x="3.5" y="5" width="17" height="14" rx="2" />
        <path d="m7.5 10 3 3-3 3" />
        <path d="M13 16.5h4" />
      </svg>
    ),
    name: 'Zero-Infrastructure',
    desc: 'One Go binary, one SQLite file. No Python, no Postgres, no Docker — embeddings are computed locally on CPU with ONNX Runtime.',
    tags: ['single binary', 'CGO', 'SQLite'],
  },
  {
    num: '/04',
    icon: (
      <svg {...iconProps}>
        <rect x="4" y="4" width="16" height="7" rx="1.5" />
        <rect x="4" y="13" width="16" height="7" rx="1.5" />
        <path d="M8 7.5h.01" />
        <path d="M8 16.5h.01" />
      </svg>
    ),
    name: 'MCP-Native',
    desc: 'Twelve self-documenting tools over HTTP/SSE, built for Claude Desktop, Cursor, and any MCP client — not for human browsing.',
    tags: ['HTTP/SSE', '12 tools', 'agents'],
  },
  {
    num: '/05',
    icon: (
      <svg {...iconProps}>
        <path d="m9 15 6-6" />
        <path d="M11 6.5 13.5 4a3.54 3.54 0 0 1 5 5L16 11.5" />
        <path d="M13 17.5 10.5 20a3.54 3.54 0 0 1-5-5L8 12.5" />
      </svg>
    ),
    name: 'Cross-Domain Linking',
    desc: 'The same entity across HR, IT, and Product is linked during ingestion — every link carries its method, confidence, and evidence.',
    tags: ['rule / equals / llm', 'provenance'],
  },
  {
    num: '/06',
    icon: (
      <svg {...iconProps}>
        <path d="M5 4v6.5" />
        <path d="M5 15.5V20" />
        <path d="M12 4v2.5" />
        <path d="M12 11.5V20" />
        <path d="M19 4v10.5" />
        <path d="M19 19v1" />
        <circle cx="5" cy="13" r="2" />
        <circle cx="12" cy="9" r="2" />
        <circle cx="19" cy="16.5" r="2" />
      </svg>
    ),
    name: 'Domain-Based Config',
    desc: 'Sources, ontologies, and extraction methods are declared per domain in XML — HR, IT, Product, and your own vocabulary.',
    tags: ['ontology xml', 'domains', 'sources'],
  },
];

interface FlowStep {
  readonly title: string;
  readonly desc: string;
}

const INGESTION_STEPS: readonly FlowStep[] = [
  {
    title: 'Parse & chunk',
    desc: 'Markdown, JSON, MediaWiki, HTML, and unstructured text — header-based or fixed-size chunking with overlap.',
  },
  {
    title: 'Embed & store',
    desc: 'ONNX Runtime generates L2-normalized vectors per chunk in sqlite-vec; text goes to FTS5 via sync triggers.',
  },
  {
    title: 'NER & entities',
    desc: 'Regex ontologies, offline statistical NER, or LLM extraction. Entities are deduplicated and linked to their chunks.',
  },
  {
    title: 'Facts & graph',
    desc: 'Subject–predicate–object facts gain confidence scores and source quotes; the graph and the hybrid index are built in the same pass.',
  },
];

const SEARCH_STEPS: readonly FlowStep[] = [
  {
    title: 'Parallel search',
    desc: 'A tools/call search runs BM25 (FTS5) and cosine KNN (sqlite-vec) concurrently over the whole knowledge base.',
  },
  {
    title: 'RRF fusion',
    desc: 'Reciprocal Rank Fusion (k=20) merges both rankings — no score normalization between lexicon and cosine.',
  },
  {
    title: 'Enrich & rerank',
    desc: 'Each hit gets document metadata and its entities attached; authority and freshness boosts reorder the tail.',
  },
  {
    title: 'Graph expansion',
    desc: 'BFS from the result entities pulls in related edges and approved facts before the answer leaves the server.',
  },
];

const QUISTEPS: readonly {cmd: string; title: string; desc: string}[] = [
  {
    cmd: 'make build',
    title: 'Build',
    desc: 'CGO compiles SQLite with FTS5 and sqlite-vec into a single self-contained bin/synopsis.',
  },
  {
    cmd: 'make sync',
    title: 'Sync',
    desc: 'Drop cleaned documents into documents/ — parsing, chunking, local ONNX embeddings, NER, facts, graph, and index land in one data/knowledge.db.',
  },
  {
    cmd: 'make run',
    title: 'Serve',
    desc: 'The MCP server starts on :8080 with /sse, /message, and /health — initial sync plus a file watcher keep it fresh.',
  },
];

const QUICKSTART_LINES: readonly TermLine[] = [
  {text: '$ make build', tone: 'cmd'},
  {text: '✓ bin/synopsis — single self-contained binary', tone: 'ok'},
  {text: '$ make sync', tone: 'cmd'},
  {text: '✓ 1,284 parsed · 12,902 embedded (onnx · bge-small)', tone: 'ok'},
  {text: '✓ 3,411 entities · 9,027 facts · graph + fts5 ready', tone: 'ok'},
  {text: '$ make run', tone: 'cmd'},
  {text: '✓ mcp server → http://localhost:8080/sse', tone: 'ok'},
  {text: '$ curl -s http://localhost:8080/health', tone: 'cmd'},
  {text: '{"status":"ok"}', tone: 'hl'},
];

interface McpTool {
  readonly name: string;
  readonly cat: string;
  readonly href: string;
  readonly desc: string;
}

const MCP_TOOLS_BASE = '/docs/reference/mcp-tools';

const MCP_TOOLS: readonly McpTool[] = [
  {name: 'search', cat: 'search', href: `${MCP_TOOLS_BASE}/search`, desc: 'Hybrid lexical (FTS5/BM25) + semantic (vector) search, fused with RRF — the primary read path for agents.'},
  {name: 'catalog_overview', cat: 'catalog', href: `${MCP_TOOLS_BASE}/catalog-overview`, desc: 'Aggregate knowledge-base stats — documents, chunks, entities, facts, distributions, graph size.'},
  {name: 'catalog_documents', cat: 'catalog', href: `${MCP_TOOLS_BASE}/catalog-documents`, desc: 'List ingested documents with cursor pagination and domain, type, and name filters.'},
  {name: 'catalog_entities', cat: 'catalog', href: `${MCP_TOOLS_BASE}/catalog-entities`, desc: 'List knowledge-graph entities with pagination and type, domain, and name filters.'},
  {name: 'search_entities_by_type', cat: 'catalog', href: `${MCP_TOOLS_BASE}/search-entities-by-type`, desc: 'All entities of one type, paginated, with an optional domain filter.'},
  {name: 'search_facts', cat: 'catalog', href: `${MCP_TOOLS_BASE}/search-facts`, desc: 'Search facts by predicate, entity name, status, and domain — triples with resolved entities.'},
  {name: 'get_document_context', cat: 'retrieval', href: `${MCP_TOOLS_BASE}/get-document-context`, desc: 'Full context of one document — metadata, chunks with offsets, entities, approved fact IDs.'},
  {name: 'get_chunk_by_id', cat: 'retrieval', href: `${MCP_TOOLS_BASE}/get-chunk-by-id`, desc: 'A single chunk with full text, offsets, parent document, and associated entities.'},
  {name: 'get_fact_by_id', cat: 'retrieval', href: `${MCP_TOOLS_BASE}/get-fact-by-id`, desc: 'One fact triple with subject/object details, status, validity, and source quotes.'},
  {name: 'get_entity_dossier', cat: 'graph', href: `${MCP_TOOLS_BASE}/get-entity-dossier`, desc: 'Complete dossier — approved facts with quotes, source documents, neighbors, cross-domain links.'},
  {name: 'get_entity_relations', cat: 'graph', href: `${MCP_TOOLS_BASE}/get-entity-relations`, desc: 'BFS traversal of the in-memory graph — nodes, edges, relation types, counters.'},
  {name: 'get_entity_links', cat: 'graph', href: `${MCP_TOOLS_BASE}/get-entity-links`, desc: 'Cross-domain links with method, confidence, and evidence — straight from the database.'},
];

const USE_CASES: readonly {tag: string; title: string; desc: string; chips: readonly string[]}[] = [
  {
    tag: 'HR',
    title: 'People operations',
    desc: 'Vacation, hiring, benefits — ask “how does the policy apply to contractors?” and get the answer with the exact source quote.',
    chips: ['policy', 'role', 'process'],
  },
  {
    tag: 'IT',
    title: 'Infrastructure ops',
    desc: 'Runbooks, incidents, change records — find the last fix for a service outage across the wiki and ticket history.',
    chips: ['service', 'incident', 'runbook'],
  },
  {
    tag: 'Product',
    title: 'Product teams',
    desc: 'Specs, roadmaps, feedback — keep features, decisions, and owners consistent in one queryable place.',
    chips: ['feature', 'decision', 'release'],
  },
  {
    tag: 'Engineering',
    title: 'Engineering',
    desc: 'Design docs, ADRs, post-mortems — the same system or person linked across code, prose, and tickets.',
    chips: ['adr', 'system', 'post-mortem'],
  },
];

const TECH_ROWS: readonly {name: string; role: string; why: string}[] = [
  {name: 'Go 1.25 (CGO)', role: 'Single self-contained binary', why: 'One artifact to ship — SQLite FTS5 and sqlite-vec are compiled in at build time.'},
  {name: 'SQLite', role: 'Storage + FTS5 + sqlite-vec', why: 'Zero infrastructure; WAL mode keeps serving reads while ingestion writes.'},
  {name: 'ONNX Runtime', role: 'Local embeddings on CPU', why: 'No Python, no API keys — INT8-quantized models run fully offline.'},
  {name: 'MCP over HTTP/SSE', role: 'Agent access layer', why: 'Native integration with Claude Desktop and Cursor; declarative, self-documenting tools.'},
  {name: 'Zig 0.14+', role: 'Cross-compilation', why: 'Linux, macOS, and Windows binaries from a single source tree.'},
];

/* ============================= hooks ============================= */

function useRevealOnScroll(): void {
  useEffect(() => {
    const elements = Array.from(
      document.querySelectorAll<HTMLElement>('[data-reveal]'),
    );
    if (elements.length === 0) return;
    // Reveal state lives in the data-revealed attribute, not a class: React
    // rewrites className from its vdom on every re-render (e.g. the accordion
    // toggling capOpen), which would wipe a manually-added class and hide
    // already-revealed content. Attributes absent from JSX props are left
    // untouched by React, so data-revealed survives re-renders.
    const markRevealed = (el: Element): void => {
      el.setAttribute('data-revealed', 'true');
    };
    if (typeof IntersectionObserver === 'undefined') {
      elements.forEach(markRevealed);
      return;
    }
    const observer = new IntersectionObserver(
      (entries) => {
        for (const entry of entries) {
          if (entry.isIntersecting) {
            markRevealed(entry.target);
            observer.unobserve(entry.target);
          }
        }
      },
      {threshold: 0.12},
    );
    elements.forEach((el) => observer.observe(el));
    return () => observer.disconnect();
  }, []);
}

function useTerminalTypewriter(lines: readonly TermLine[]) {
  const bodyRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const body = bodyRef.current;
    if (body === null) return;

    const reducedMotion = window.matchMedia(
      '(prefers-reduced-motion: reduce)',
    ).matches;

    const timeouts: number[] = [];
    const intervals: number[] = [];
    let cancelled = false;
    const after = (fn: () => void, ms: number): void => {
      timeouts.push(window.setTimeout(() => {
        if (!cancelled) fn();
      }, ms));
    };

    body.textContent = '';
    const cursor = document.createElement('span');
    cursor.className = styles.cursor;

    const makeLine = (line: TermLine): HTMLElement => {
      const row = document.createElement('div');
      row.className = styles.tline;
      const span = document.createElement('span');
      span.className = TONE_CLASS[line.tone];
      row.appendChild(span);
      body.appendChild(row);
      return span;
    };

    if (reducedMotion) {
      for (const line of lines) {
        makeLine(line).textContent = line.text;
      }
      body.lastElementChild?.appendChild(cursor);
    } else {
      let lineIndex = 0;
      const typeNextLine = (): void => {
        if (lineIndex >= lines.length) return;
        const line = lines[lineIndex];
        const span = makeLine(line);
        body.lastElementChild?.appendChild(cursor);
        let charIndex = 0;
        intervals.push(
          window.setInterval(() => {
            if (cancelled) return;
            charIndex += 1;
            span.textContent = line.text.slice(0, charIndex);
            if (charIndex >= line.text.length) {
              window.clearInterval(intervals[intervals.length - 1]);
              lineIndex += 1;
              if (lineIndex < lines.length) {
                after(typeNextLine, line.tone === 'cmd' ? 420 : 240);
              }
            }
          }, line.tone === 'cmd' ? 28 : 11),
        );
      };
      after(typeNextLine, 700);
    }

    return () => {
      cancelled = true;
      timeouts.forEach((id) => window.clearTimeout(id));
      intervals.forEach((id) => window.clearInterval(id));
    };
  }, [lines]);

  return bodyRef;
}

/* ============================= sections ============================= */

function Hero(): ReactNode {
  const termBodyRef = useTerminalTypewriter(TERMINAL_LINES);
  return (
    <header className={styles.hero}>
      <div className={styles.wrap}>
        <div className={styles.heroGrid}>
          <div>
            <p className={styles.eyebrow}>
              <b>[ open source ]</b> single go binary · sqlite · mcp protocol
            </p>
            <h1 className={styles.heroTitle}>
              Synopsis
              <br />
              <span className={styles.hl}>[MEMEX]</span>
            </h1>
            <p className={styles.heroSub}>
              <strong>Structured information for AI agents via MCP.</strong>
              One Go binary combining hybrid search — BM25 and vectors, fused
              with RRF — and an in-memory knowledge graph. No Python, no
              Postgres, no Docker.
            </p>
            <div className={styles.heroActions}>
              <Link className={`${styles.btn} ${styles.btnPrimary}`} to="/docs/intro">
                GET STARTED →
              </Link>
              <a
                className={`${styles.btn} ${styles.btnGhost}`}
                href={GITHUB_URL}
                target="_blank"
                rel="noreferrer">
                GITHUB ↗
              </a>
            </div>
            <div className={styles.heroMeta}>
              <span>
                Engine — <b>Go 1.25</b>
              </span>
              <span>
                Protocol — <b>MCP / SSE</b>
              </span>
              <span>
                Store — <b>SQLite</b>
              </span>
              <span>
                Embeddings — <b>ONNX</b>
              </span>
            </div>
          </div>

          <div className={styles.termCol}>
            <div className={styles.statusPill}>
              <i aria-hidden="true" />
              MCP ONLINE
            </div>
            <div className={styles.terminal} role="img" aria-label="Terminal showing synopsis built, synced, and served as an MCP server">
              <div className={styles.termHead}>
                <span className={styles.tdotR} aria-hidden="true" />
                <span className={styles.tdotY} aria-hidden="true" />
                <span className={styles.tdotG} aria-hidden="true" />
                <span className={styles.termTitle}>synopsis — zsh · :8080/sse</span>
              </div>
              <div className={styles.termBody} ref={termBodyRef} />
            </div>
          </div>
        </div>
      </div>
    </header>
  );
}

function Ticker(): ReactNode {
  return (
    <div className={styles.ticker} aria-hidden="true">
      <div className={styles.tickerTrack}>
        {[0, 1].map((group) => (
          <div key={group} className={styles.tickerGroup}>
            {TICKER_ITEMS.map((item) => (
              <span key={item} className={styles.tickItem}>
                {item}
              </span>
            ))}
          </div>
        ))}
      </div>
    </div>
  );
}

function PipelineSection(): ReactNode {
  const [openIdx, setOpenIdx] = useState(0);
  const bodyRefs = useRef<(HTMLDivElement | null)[]>([]);

  useEffect(() => {
    bodyRefs.current.forEach((el, i) => {
      if (el) el.style.maxHeight = i === openIdx ? `${el.scrollHeight}px` : '';
    });
  }, [openIdx]);

  useEffect(() => {
    const onResize = () => {
      bodyRefs.current.forEach((el, i) => {
        if (el && i === openIdx) el.style.maxHeight = `${el.scrollHeight}px`;
      });
    };
    window.addEventListener('resize', onResize);
    return () => window.removeEventListener('resize', onResize);
  }, [openIdx]);

  return (
    <section className={styles.pipeline} id="pipeline">
      <div className={styles.wrap}>
        <p className={styles.secTag} data-reveal>
          <b>//</b> 01 · pipeline
        </p>
        <h2 className={styles.display} data-reveal>
          Clean data in.
          <br />
          MCP tools out.
        </h2>
        <p className={styles.sectionLead} data-reveal>
          Synopsis is one stage of a larger information pipeline — not a
          standalone assistant and not a search platform. Everything between
          normalized documents and 12 MCP tools belongs here.
        </p>
        <div className={styles.capList}>
          {PIPELINE_STAGES.map((stage, i) => {
            const open = i === openIdx;
            return (
              <div
                key={stage.num}
                className={`${styles.cap} ${open ? styles.capOpen : ''} ${['', styles.d1, styles.d2, styles.d3][i] ?? ''}`}
                data-reveal>
                <button
                  type="button"
                  className={styles.capHead}
                  aria-expanded={open}
                  aria-controls={`pipeline-body-${stage.num}`}
                  onClick={() => setOpenIdx(open ? -1 : i)}>
                  <span className={styles.capNum}>/{stage.num}</span>
                  <span className={styles.capTitle}>{stage.title}</span>
                  <span className={styles.capTags}>{stage.meta}</span>
                  <span className={styles.capIcon} aria-hidden="true">
                    +
                  </span>
                </button>
                <div
                  id={`pipeline-body-${stage.num}`}
                  ref={(el) => {
                    bodyRefs.current[i] = el;
                  }}
                  className={styles.capBody}>
                  <div className={styles.capInner}>
                    <div>
                      <p>{stage.desc}</p>
                      <div className={styles.chipRow}>
                        {stage.chips.map((chip) => (
                          <span key={chip} className={styles.chip}>
                            {chip}
                          </span>
                        ))}
                      </div>
                    </div>
                  </div>
                </div>
              </div>
            );
          })}
        </div>
      </div>
    </section>
  );
}

function FeaturesSection(): ReactNode {
  return (
    <section className={styles.features} id="features">
      <div className={styles.wrap}>
        <p className={styles.secTag} data-reveal>
          <b>//</b> 02 · capabilities
        </p>
        <h2 className={styles.display} data-reveal>
          One job, done well.
        </h2>
        <p className={styles.sectionLead} data-reveal>
          Six systems working inside a single process — strict boundaries, no
          services to operate between them.
        </p>
        <div className={styles.featGrid}>
          {FEATURES.map((feature, i) => (
            <article
              key={feature.name}
              className={`${styles.featCard} ${['', styles.d1, styles.d2][i % 3] ?? ''}`}
              data-reveal>
              <div className={styles.featTop}>
                <span className={styles.featIcon}>{feature.icon}</span>
                <span className={styles.featNum}>{feature.num}</span>
              </div>
              <h3>{feature.name}</h3>
              <p>{feature.desc}</p>
              <div className={styles.chipRow}>
                {feature.tags.map((tag) => (
                  <span key={tag} className={styles.chip}>
                    {tag}
                  </span>
                ))}
              </div>
            </article>
          ))}
        </div>
      </div>
    </section>
  );
}

function HowSection(): ReactNode {
  const panel = (
    badge: string,
    title: string,
    steps: readonly FlowStep[],
    delay: string,
  ): ReactNode => (
    <div className={`${styles.howPanel} ${delay}`} data-reveal>
      <div className={styles.howPanelHead}>
        <span className={styles.howBadge}>{badge}</span>
        <h3>{title}</h3>
      </div>
      <div className={styles.howSteps}>
        {steps.map((step) => (
          <div key={step.title} className={styles.howStep}>
            <h4>{step.title}</h4>
            <p>{step.desc}</p>
          </div>
        ))}
      </div>
    </div>
  );
  return (
    <section className={styles.how} id="how">
      <div className={styles.wrap}>
        <p className={styles.secTag} data-reveal>
          <b>//</b> 03 · how it works
        </p>
        <h2 className={styles.display} data-reveal>
          Ingestion → graph → search.
        </h2>
        <p className={styles.sectionLead} data-reveal>
          Two paths, one process. The sync pass builds the knowledge base; the
          query pass answers agents with ranked, enriched, provenance-carrying
          results.
        </p>
        <div className={styles.howGrid}>
          {panel('1', 'Ingestion pipeline', INGESTION_STEPS, '')}
          {panel('2', 'Query execution', SEARCH_STEPS, styles.d2)}
        </div>
      </div>
    </section>
  );
}

function QuickstartSection(): ReactNode {
  return (
    <section className={styles.quickstart} id="quickstart">
      <div className={styles.wrap}>
        <p className={styles.secTag} data-reveal>
          <b>//</b> 04 · quick start
        </p>
        <h2 className={styles.display} data-reveal>
          Clone to MCP tools in five minutes.
        </h2>
        <div className={styles.qsGrid}>
          <div>
            {QUISTEPS.map((step, i) => (
              <div
                key={step.cmd}
                className={`${styles.qsStep} ${['', styles.d1, styles.d2][i] ?? ''}`}
                data-reveal>
                <span className={styles.qsNum} aria-hidden="true">
                  {String(i + 1).padStart(2, '0')}
                </span>
                <div>
                  <h3>
                    {step.title}{' '}
                    <code className={styles.qsCmd}>{step.cmd}</code>
                  </h3>
                  <p>{step.desc}</p>
                </div>
              </div>
            ))}
            <p data-reveal className={styles.d3}>
              <Link className={styles.toolLink} to="/docs/quickstart">
                Read the full quickstart →
              </Link>
            </p>
          </div>
          <div className={styles.d2} data-reveal>
            <div className={styles.qsCode}>
              <div className={styles.qsCodeHead}>
                <span className={styles.tdotR} aria-hidden="true" />
                <span className={styles.tdotY} aria-hidden="true" />
                <span className={styles.tdotG} aria-hidden="true" />
                <span className={styles.qsCodeTitle}>synopsis — make</span>
              </div>
              <pre className={styles.qsCodePre}>
                {QUICKSTART_LINES.map((line) => (
                  <span
                    key={line.text}
                    className={`${styles.codeLine} ${TONE_CLASS[line.tone]}`}>
                    {line.text}
                  </span>
                ))}
              </pre>
            </div>
            <div className={styles.qsJson}>
              <p className={styles.qsJsonLabel}>
                <b>→</b> first mcp call — POST /message · tools/call
              </p>
              <pre className={styles.qsCodePre}>
                {'{\n  "jsonrpc": "2.0",\n  "id": 1,\n  "method": "tools/call",\n  "params": {\n    "name": '}
                <span className={styles.tHl}>{"search"}</span>
                {',\n    "arguments": {\n      "query": "vacation policy",\n      "domain": "hr"\n    }\n  }\n}'}
              </pre>
            </div>
          </div>
        </div>
      </div>
    </section>
  );
}

function ToolsSection(): ReactNode {
  return (
    <section className={styles.tools} id="tools">
      <div className={styles.wrap}>
        <p className={styles.secTag} data-reveal>
          <b>//</b> 05 · mcp tools
        </p>
        <h2 className={styles.display} data-reveal>
          Twelve tools. One protocol.
        </h2>
        <p className={styles.sectionLead} data-reveal>
          Everything an agent needs to read the knowledge base — declarative
          JSON schemas, cursor pagination, and full provenance on every
          answer.
        </p>
        <div className={styles.toolsGrid}>
          {MCP_TOOLS.map((tool, i) => (
            <Link
              key={tool.name}
              to={tool.href}
              className={`${styles.toolCard} ${['', styles.d1, styles.d2][i % 3] ?? ''}`}
              data-reveal>
              <span className={styles.toolTop}>
                <span className={styles.toolIdx}>{String(i + 1).padStart(2, '0')}</span>
                <span className={styles.toolCat}>{tool.cat}</span>
              </span>
              <span className={styles.toolName}>{tool.name}</span>
              <span className={styles.toolDesc}>{tool.desc}</span>
              <span className={styles.toolLink}>docs →</span>
            </Link>
          ))}
        </div>
      </div>
    </section>
  );
}

function UseCasesSection(): ReactNode {
  return (
    <section className={styles.usecases} id="usecases">
      <div className={styles.wrap}>
        <p className={styles.secTag} data-reveal>
          <b>//</b> 06 · use cases
        </p>
        <h2 className={styles.display} data-reveal>
          Built for how teams actually store knowledge.
        </h2>
        <p className={styles.sectionLead} data-reveal>
          Domain tags decide what gets extracted and how it links — the same
          engine serves every department.
        </p>
        <div className={styles.ucGrid}>
          {USE_CASES.map((useCase, i) => (
            <article
              key={useCase.tag}
              className={`${styles.ucCard} ${['', styles.d1, styles.d2, styles.d3][i] ?? ''}`}
              data-reveal>
              <span className={styles.ucTag}>{useCase.tag}</span>
              <h3>{useCase.title}</h3>
              <p>{useCase.desc}</p>
              <div className={styles.chipRow}>
                {useCase.chips.map((chip) => (
                  <span key={chip} className={styles.chip}>
                    {chip}
                  </span>
                ))}
              </div>
            </article>
          ))}
        </div>
      </div>
    </section>
  );
}

function TechSection(): ReactNode {
  return (
    <section className={styles.tech} id="tech">
      <div className={styles.wrap}>
        <p className={styles.secTag} data-reveal>
          <b>//</b> 07 · tech stack
        </p>
        <h2 className={styles.display} data-reveal>
          Chosen for zero operations, not fashion.
        </h2>
        <p className={styles.sectionLead} data-reveal>
          Each component exists so the whole system stays one process and one
          file — and so it can still be rebuilt by a single engineer.
        </p>
        <div className={styles.tableWrap} data-reveal>
          <table className={styles.table}>
            <thead>
              <tr>
                <th scope="col">Component</th>
                <th scope="col">Role</th>
                <th scope="col">Why</th>
              </tr>
            </thead>
            <tbody>
              {TECH_ROWS.map((row) => (
                <tr key={row.name}>
                  <th scope="row">{row.name}</th>
                  <td>{row.role}</td>
                  <td>{row.why}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </div>
    </section>
  );
}

function CtaSection(): ReactNode {
  return (
    <section className={styles.cta} id="cta">
      <div className={styles.wrap}>
        <p className={styles.secTag} data-reveal>
          <b>//</b> 08 · get started
        </p>
        <h2 className={styles.ctaTitle} data-reveal>
          Give your agents real context.
        </h2>
        <p className={styles.ctaCopy} data-reveal>
          Point any MCP client at localhost:8080/sse. Clean documents in, a
          queryable knowledge base out — about five minutes of your time.
        </p>
        <div className={styles.ctaActions} data-reveal>
          <Link className={`${styles.btn} ${styles.btnPrimary}`} to="/docs/quickstart">
            GET STARTED →
          </Link>
          <a
            className={`${styles.btn} ${styles.btnGhost}`}
            href={GITHUB_URL}
            target="_blank"
            rel="noreferrer">
            GITHUB ↗
          </a>
        </div>
        <p className={styles.ctaPrompt} data-reveal>
          <b>$</b> synopsis serve — mcp http://localhost:8080/sse
        </p>
      </div>
    </section>
  );
}

/* ============================= page ============================= */

export default function Home(): ReactNode {
  useRevealOnScroll();

  return (
    <Layout
      title="Structured information for AI agents via MCP"
      description="Synopsis[MEMEX] is a zero-infrastructure knowledge base: hybrid search and an in-memory knowledge graph in one Go binary, exposed as 12 MCP tools.">
      <div className={styles.page}>
        <div className={styles.noise} aria-hidden="true" />
        <noscript>
          <style>{'[data-reveal]{opacity:1 !important;transform:none !important}'}</style>
        </noscript>
        <main>
          <Hero />
          <Ticker />
          <PipelineSection />
          <FeaturesSection />
          <HowSection />
          <QuickstartSection />
          <ToolsSection />
          <UseCasesSection />
          <TechSection />
          <CtaSection />
        </main>
      </div>
    </Layout>
  );
}
