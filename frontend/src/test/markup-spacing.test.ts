import { describe, it, expect } from 'vitest';
import { readdirSync, readFileSync } from 'node:fs';
import path from 'node:path';

// Svelte trims the whitespace at a block boundary. So in
//
//   add a fresh credential{#if canWrite} with <span>+ Add credential</span>{/if}.
//
// the space that opens the {#if} body is dropped and the rendered page reads
// "credentialwith". Nothing catches it: it compiles, svelte-check is happy, and
// the text is only wrong in the pixels -- this one shipped, and was found by
// looking at a docs screenshot of the page.
//
// The fix is to keep the space out of the boundary, normally by giving each
// branch the whole sentence.
const BOUNDARY_SWALLOWS_A_SPACE = /[^\s>}"']\{#(?:if|each)\b[^}]*\}[ \t]/;

function svelteFiles(dir: string): string[] {
  const out: string[] = [];
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const p = path.join(dir, entry.name);
    if (entry.isDirectory()) out.push(...svelteFiles(p));
    else if (entry.name.endsWith('.svelte')) out.push(p);
  }
  return out;
}

describe('svelte block boundaries', () => {
  const root = path.resolve(import.meta.dirname, '..');
  const files = svelteFiles(root);

  // A scan that finds no files reads exactly like a scan that found no
  // problems, so the file count is asserted before the contents are.
  it('found the components to scan', () => {
    expect(files.length).toBeGreaterThan(30);
  });

  it.each(files.map((f) => path.relative(root, f)))(
    'does not open an {#if} body with a space that Svelte will drop: %s',
    (rel) => {
      const lines = readFileSync(path.join(root, rel), 'utf8').split('\n');
      const offenders = lines
        .map((line, i) => ({ line, n: i + 1 }))
        .filter(({ line }) => BOUNDARY_SWALLOWS_A_SPACE.test(line))
        .map(({ line, n }) => `${rel}:${n}: ${line.trim()}`);
      expect(offenders).toEqual([]);
    }
  );
});
