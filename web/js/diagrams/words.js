// Status words for the diagrams, kept free of DOM imports so the pure
// modules can use them under node. Matches statusText() in lib/ui.js and
// adds the two states only a diagram sees: an id absent from the snapshot,
// and a derived fact the rules could not determine.

const WORDS = {
  ok: 'ok',
  absent: 'absent',
  permission_denied: 'permission denied',
  unsupported: 'unsupported',
  failed: 'failed',
  not_collected: 'not collected',
  unknown: 'unknown',
  missing: 'missing',
};

export function statusWords(status) {
  return WORDS[status] ?? String(status);
}
