export const areas = [ ['contact', 'Contact'], ['money', 'Money'], ['data', 'Data'], ['age_gating', 'Age gating'], ['content', 'Content'], ['developer_record', 'Developer record'] ] as const;
export type Area = typeof areas[number][0];
export const verdicts = { yes: 'Yes', yes_with_settings: 'Yes, with these settings', no: 'No', unclear: 'Not enough evidence' };
export const levels = { ok: 'OK', ok_with_setting: 'OK with a setting', flag: 'Flag', unknown: 'Not enough evidence' };
export type Finding = { id: string; area: Area; claim: string; stance: 'risk' | 'mitigation' | 'neutral'; quote: string; source: { url: string; kind: string; index?: number; title?: string; fetched_at: string } };
export type Grade = { area: Area; level: keyof typeof levels; findings: string[]; reason?: string; setting?: string };
export type Card = { id: string; title: string; kind: string; kind_label: string; age: number; checklist_version: string; checked_at: string; collected_at: string; verdict: keyof typeof verdicts; grades: Grade[]; settings: string[]; findings: Finding[]; alternatives: { title: string; reason: string; card_id: string; verdict: keyof typeof verdicts; status: string }[]; alternatives_note?: string; stats: { sources: number; reviews: number; review_source: string; chunks: number; failed_chunks: number; drops: { bad_area: number; bad_stance: number; no_quote: number; unquoted: number; duplicates: number }; conflicted_areas: Area[]; missing_kinds: string[]; tavily_credits: number; read_cost_nano: number; judge_cost_nano: number; reader_model: string; judge_model: string } };
const record = (v: unknown): v is Record<string, unknown> => typeof v === 'object' && v !== null && !Array.isArray(v);
const text = (v: unknown): v is string => typeof v === 'string' && !!v.trim();
const strings = (v: unknown): v is string[] => Array.isArray(v) && v.every(text);
const count = (v: unknown) => Number.isSafeInteger(v) && Number(v) >= 0;
const date = (v: unknown) => text(v) && Number.isFinite(Date.parse(v));
const area = (v: unknown) => areas.some(([id]) => id === v);
export const cardID = (v: unknown): v is string => typeof v === 'string' && v.length === 34 && /^c_[0-9a-f]{32}$/.test(v);
function require(value: unknown): asserts value { if (!value) throw new Error('Invalid card'); }
const optionalText = (v: unknown) => v === undefined || typeof v === 'string';
function safeURL(v: unknown) { if (!text(v)) return false; try { const url = new URL(v); return ['https:', 'http:'].includes(url.protocol) && !url.username && !url.password; } catch { return false; } }
/** Check structure and citation integrity, never recompute the server's verdict. */
export function parseCard(value: unknown, id: string): Card {
 require(record(value));
 // Go nil slices encode as null; an explicitly empty collection is valid.
 const c = { ...value };
 for (const key of ['settings', 'findings']) if (c[key] === null) c[key] = [];
 if (Array.isArray(c.grades)) c.grades = c.grades.map(g => record(g) && g.findings === null ? { ...g, findings: [] } : g);
 if (record(c.stats)) { c.stats = { ...c.stats }; for (const key of ['conflicted_areas', 'missing_kinds']) if ((c.stats as Record<string, unknown>)[key] === null) (c.stats as Record<string, unknown>)[key] = []; }
 require(cardID(id) && c.id === id && text(c.title) && ['ios', 'android', 'console_pc', 'youtube', 'unknown'].includes(String(c.kind)) && text(c.kind_label));
 require(c.kind !== 'unknown' || c.kind_label === 'kind unknown');
 require(Number.isInteger(c.age) && Number(c.age) >= 2 && Number(c.age) <= 17 && text(c.checklist_version) && date(c.checked_at) && date(c.collected_at));
 require(typeof c.verdict === 'string' && Object.hasOwn(verdicts, c.verdict) && strings(c.settings) && Array.isArray(c.findings));
 const findings = new Map<string, Record<string, unknown>>();
 for (const f of c.findings) {
  require(record(f) && text(f.id) && /^f_[A-Za-z0-9_-]+$/.test(f.id) && !findings.has(f.id) && area(f.area) && text(f.claim) && ['risk', 'mitigation', 'neutral'].includes(String(f.stance)) && text(f.quote) && record(f.source));
  require(safeURL(f.source.url) && text(f.source.kind) && date(f.source.fetched_at) && optionalText(f.source.title) && (f.source.index === undefined || count(f.source.index)));
  findings.set(f.id, f);
 }
 require(Array.isArray(c.grades) && c.grades.length === 6); const seen = new Set();
 for (const g of c.grades) {
  require(record(g) && area(g.area) && !seen.has(g.area) && typeof g.level === 'string' && Object.hasOwn(levels, g.level) && strings(g.findings) && optionalText(g.reason) && optionalText(g.setting));
  seen.add(g.area);
  require(new Set(g.findings).size === g.findings.length && g.findings.every(fid => findings.get(fid)?.area === g.area));
  require(g.level === 'unknown' || g.findings.length > 0);
  require(g.level !== 'flag' || g.findings.some(fid => findings.get(fid)?.stance === 'risk'));
  require(g.level !== 'ok_with_setting' || (text(g.setting) && g.findings.length > 0));
 }
 require(new Set(c.settings).size === c.settings.length);
 require(c.settings.every(s => (c.grades as Record<string, unknown>[]).some(g => g.level === 'ok_with_setting' && g.setting === s)));
 require(Array.isArray(c.alternatives) && optionalText(c.alternatives_note));
 const alternativeIDs = new Set();
 for (const a of c.alternatives) { require(record(a) && typeof a.status === 'string'); if (a.status === 'checked') { require(!alternativeIDs.has(a.card_id)); alternativeIDs.add(a.card_id); require(text(a.title) && text(a.reason) && cardID(a.card_id) && typeof a.verdict === 'string' && Object.hasOwn(verdicts, a.verdict)); } }
 require(record(c.stats)); const s = c.stats;
 require(['sources', 'reviews', 'chunks', 'failed_chunks', 'tavily_credits', 'read_cost_nano', 'judge_cost_nano'].every(k => count(s[k])) && ['', 'feed', 'page'].includes(String(s.review_source)) && text(s.reader_model) && text(s.judge_model) && record(s.drops));
 require(['bad_area', 'bad_stance', 'no_quote', 'unquoted', 'duplicates'].every(k => count((s.drops as Record<string, unknown>)[k])) && strings(s.missing_kinds) && Array.isArray(s.conflicted_areas) && s.conflicted_areas.every(area));
 return c as unknown as Card;
}
export function evidence(card: Card, grade: Grade) { return grade.findings.map(id => card.findings.find(f => f.id === id)!); }
export function leadEvidence(card: Card) { for (const [id] of areas) { const grade = card.grades.find(g => g.area === id)!; if (grade.level === 'flag') { const finding = evidence(card, grade).find(f => f.stance === 'risk'); if (finding) return { grade, finding }; } } }
export function sourceLabel(f: Finding) { if (f.source.kind === 'review') return f.source.index ? `Review ${f.source.index}` : 'Review'; if (f.source.kind === 'policy') return 'Privacy policy'; if (['forum', 'reddit'].includes(f.source.kind)) return 'Forum thread'; return new URL(f.source.url).hostname; }
