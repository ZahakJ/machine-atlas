// Branch conditions in words. A lesson branch (`:::when FACT [is VALUE]`)
// is decided by one fact; this module says, for a human, which condition
// held here and what each other arm stands for: "swap is configured",
// "swap used fraction is non-zero (4.92 %)", "THP mode is always". Pure
// functions, no DOM, so they are tested under node.

import { shortValue, isTable } from './values.js';
import { statusText } from '../lib/ui.js';

// Yes/no facts read better as a statement than as "Title: yes". The pair is
// [when true, when false]. A fact not listed here falls back to its catalog
// title ("Swap configured: yes").
const STATEMENTS = {
  'derived.swap.configured': ['swap is configured', 'no swap is configured'],
  'derived.swap.compressed': ['compressed swap is present', 'no compressed swap is present'],
  'derived.numa.multi': ['there are several NUMA nodes', 'there is a single NUMA node'],
  'derived.thp.enabled': ['transparent huge pages are enabled', 'transparent huge pages are disabled'],
  'derived.cgroup.memory_limited': ['a cgroup limits memory', 'no cgroup limits memory'],
  'derived.commit.over_limit': ['committed memory exceeds the commit limit', 'committed memory is within the commit limit'],
  'derived.oom.any': ['the OOM killer has killed a process', 'the OOM killer has not killed anything'],
  'derived.probe.aslr_differs': ['the probe’s layout differed between runs', 'the probe’s layout was the same in both runs'],
  'derived.psi.available': ['pressure stall information is available', 'pressure stall information is not available'],
  'cpu.flags.la57': ['the CPU supports 5-level paging', 'the CPU does not support 5-level paging'],
  'mem.zswap.enabled': ['zswap is enabled', 'zswap is disabled'],
};

/** "Swap used fraction" → "swap used fraction"; "THP mode" stays. */
export function lowerFirst(s) {
  const t = String(s ?? '');
  return /^[A-Z][a-z]/.test(t) ? t[0].toLowerCase() + t.slice(1) : t;
}

/** A value of the fact formatted the way a chip would show it. */
function fmt(value, c) {
  return shortValue({ id: c.fact, unit: c.unit, ...(c.item ?? {}), status: 'ok', value });
}

// The catalog's value types, as the kinds a truthiness test distinguishes.
const TYPE_KINDS = { bool: 'bool', int: 'number', float: 'number', limit: 'number', table: 'table', string: 'string' };

function kindOf(value) {
  if (typeof value === 'boolean') return 'bool';
  if (typeof value === 'number') return 'number';
  if (isTable(value)) return 'table';
  return 'string';
}

/**
 * What the arm `arm` ("then" | "else" | "unknown") of a branch on `fact`
 * stands for, as a clause: "no swap is configured", "THP mode is not
 * always". c: {fact, expect, title, unit, type, item}: title, unit and
 * type from the catalog, and item is the fact's
 * observation or derived fact when the snapshot has one. With `actual`
 * true the clause also states this machine's value where it adds
 * something: "swap used fraction is non-zero (4.92 %)".
 */
export function clause(c, arm, actual = false) {
  const name = lowerFirst(c.title || c.fact);
  const item = c.item;
  const hasValue = item?.status === 'ok' && item.value !== null && item.value !== undefined;
  const statement = STATEMENTS[c.fact];
  if (arm === 'unknown') {
    const what = statement ? `whether ${statement[0]}` : name;
    return `Machine Atlas cannot determine ${what}`;
  }
  const expect = c.expect;
  if (expect !== null && expect !== undefined) {
    const want = fmt(expect, c);
    if (arm === 'then') return `${name} is ${want}`;
    return actual && hasValue ? `${name} is ${fmt(item.value, c)}, not ${want}` : `${name} is not ${want}`;
  }
  // A truthiness test: true, a non-zero number, a non-empty string or table.
  const kind = hasValue ? kindOf(item.value) : statement ? 'bool' : TYPE_KINDS[c.type] ?? 'unknown';
  const now = actual && hasValue ? fmt(item.value, c) : null;
  switch (kind) {
    case 'bool':
      if (statement) return arm === 'then' ? statement[0] : statement[1];
      return `${name}: ${arm === 'then' ? 'yes' : 'no'}`;
    case 'number':
      return arm === 'then' ? `${name} is non-zero${now ? ` (${now})` : ''}` : `${name} is zero`;
    case 'table':
      return arm === 'then' ? `${name} could be read${now ? ` (${item.value.length} row${item.value.length === 1 ? '' : 's'})` : ''}` : `${name}: no rows`;
    case 'string':
      return arm === 'then' ? `${name} is ${now ?? 'set'}` : `${name} is empty`;
    default:
      return arm === 'then' ? `${name} holds` : `${name} does not hold`;
  }
}

/**
 * The reason a chosen arm is shown, as one sentence fragment for a list of
 * conditions (outermost first): "swap is configured and swap used fraction
 * is non-zero (4.92 %)". Each condition is {fact, expect, title, item,
 * chosen}.
 */
export function because(conditions) {
  const parts = conditions.map((c) => {
    if (c.chosen !== 'unknown') return clause(c, c.chosen, true);
    const why = c.item ? (c.item.status === 'unknown' ? 'unknown' : statusText(c.item.status)) : 'not in this snapshot';
    return `${clause(c, 'unknown')} (${why})`;
  });
  return joinAnd(parts);
}

/**
 * The text a generated unknown arm would show, for when it is not the one
 * chosen: what could not be determined and that the reason and source would
 * be named. (When it is chosen, the bound lesson carries the real reason.)
 */
export function unknownWording(c) {
  const src = c.item?.source?.path;
  const where = src ? `, and would name the reason and the source it tried (${src})` : ', and would name the reason and where it looked';
  return `${clause(c, 'unknown').replace('cannot', 'could not')} on this machine${where}.`;
}

function joinAnd(parts) {
  if (parts.length <= 1) return parts[0] ?? '';
  return `${parts.slice(0, -1).join(', ')} and ${parts[parts.length - 1]}`;
}

/**
 * Follows a branch whose chosen arm is nothing but another branch, so that
 * nested conditions collapse into one: returns {conditions, content} where
 * conditions are the branches from the outside in and content is the
 * innermost chosen arm.
 */
export function collapse(branch) {
  const conditions = [branch];
  let cur = branch;
  for (;;) {
    const arm = cur[cur.chosen] ?? [];
    if (arm.length === 1 && arm[0]?.t === 'branch') {
      cur = arm[0];
      conditions.push(cur);
    } else return { conditions, content: arm };
  }
}
