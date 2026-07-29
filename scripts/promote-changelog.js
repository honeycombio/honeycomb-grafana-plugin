#!/usr/bin/env node
//
// Promote the CHANGELOG's [Unreleased] section to a released version heading.
//
// Every merge to main auto-releases a patch (see version-bump.yml), but nothing
// used to rename the [Unreleased] heading — so the release notes linked users to
// a changelog whose newest section was permanently "Unreleased".
//
// Usage:
//   node scripts/promote-changelog.js <version> [file]
//
// Safe to re-run: it exits without writing if a heading for <version> already
// exists, if there is no [Unreleased] heading, or if that section is empty
// (Keep a Changelog convention — don't emit an empty version heading).

const fs = require('fs');

const version = process.argv[2];
const file = process.argv[3] || 'CHANGELOG.md';

if (!version || !/^\d+\.\d+\.\d+$/.test(version)) {
  console.error(`usage: promote-changelog.js <x.y.z> [file] (got '${version ?? ''}')`);
  process.exit(1);
}

const original = fs.readFileSync(file, 'utf8');
const escaped = version.replace(/\./g, '\\.');

if (new RegExp(`^## \\[${escaped}\\]`, 'm').test(original)) {
  console.log(`CHANGELOG: already has a [${version}] section, leaving it alone.`);
  process.exit(0);
}

const lines = original.split('\n');
const headingIdx = lines.findIndex((l) => /^## \[Unreleased\]\s*$/.test(l));
if (headingIdx === -1) {
  console.log('CHANGELOG: no ## [Unreleased] heading found, leaving it alone.');
  process.exit(0);
}

// The section runs to the next ## heading, or to the link-reference block.
let endIdx = lines.length;
for (let i = headingIdx + 1; i < lines.length; i++) {
  if (/^## /.test(lines[i]) || /^\[[^\]]+\]:\s/.test(lines[i])) {
    endIdx = i;
    break;
  }
}

if (lines.slice(headingIdx + 1, endIdx).join('\n').trim() === '') {
  console.log('CHANGELOG: [Unreleased] is empty, not creating an empty version heading.');
  process.exit(0);
}

const today = new Date().toISOString().slice(0, 10);
lines.splice(headingIdx, 1, '## [Unreleased]', '', `## [${version}] — ${today}`);
let out = lines.join('\n');

// Derive the link-reference URLs from the existing [Unreleased] line rather than
// hardcoding them, so this keeps working if the repo is ever renamed.
const refRe = /^\[Unreleased\]:\s*(\S*?\/compare\/)v\d+\.\d+\.\d+(\.\.\.HEAD)\s*$/m;
const match = out.match(refRe);
if (match) {
  const comparePrefix = match[1];
  const tagPrefix = comparePrefix.replace(/\/compare\/$/, '/releases/tag/');
  out = out.replace(
    refRe,
    `[Unreleased]: ${comparePrefix}v${version}${match[2]}\n[${version}]: ${tagPrefix}v${version}`
  );
} else {
  console.warn('CHANGELOG: no [Unreleased]: compare link found to update; add link refs by hand.');
}

fs.writeFileSync(file, out);
console.log(`CHANGELOG: promoted [Unreleased] to [${version}] — ${today}`);
