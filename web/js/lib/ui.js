// Builders for the small components every view repeats, so their markup
// matches components.css exactly in one place.

import { el } from './dom.js';

const LABEL_TEXT = {
  observed: ['Observed', 'Read from this machine.'],
  derived: ['Derived', 'Computed from observations by a stated rule.'],
  general: ['General', 'True of Linux in general, not specific to this machine.'],
  simulated: ['Simulated', 'Produced by a model; not a measurement of this machine.'],
};

/** A provenance badge: label is observed, derived, general or simulated. */
export function badge(label) {
  const [text, title] = LABEL_TEXT[label] ?? LABEL_TEXT.general;
  return el('span', { class: 'badge', 'data-label': label in LABEL_TEXT ? label : 'general', title }, text);
}

const STATUS_TEXT = {
  ok: 'ok',
  absent: 'absent',
  permission_denied: 'permission denied',
  unsupported: 'unsupported',
  failed: 'failed',
  not_collected: 'not collected',
};

/** The words a status reads as, e.g. "permission denied". */
export function statusText(status) {
  return STATUS_TEXT[status] ?? String(status);
}

/** A status pill for one of the six observation statuses. */
export function statusPill(status) {
  return el('span', { class: 'status', 'data-status': status }, statusText(status));
}
