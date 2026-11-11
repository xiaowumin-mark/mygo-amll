import assert from "node:assert/strict";
import test from "node:test";
import { fieldDifferences, normalizeCanonical, rejectionParity } from "./ttml_reference.mjs";

const word = (text, startMS, endMS) => ({ text, romanization: "", startMS, endMS, obscene: false, emptyBeat: 0, ruby: [] });
const line = (text, startMS) => ({ words: [word(text, startMS, startMS + 1000)], translation: "", romanization: "", startMS, endMS: startMS + 1000, background: false, duet: false });

test("only declared metadata differences and stable line order normalize", () => {
  const first = line("first", 3000);
  const tied = line("tied", 3000);
  const earlier = line("earlier", 1000);
  const go = normalizeCanonical({ lines: [first, tied, earlier], metadata: [
    ["title", ["Synthetic"]], ["artists", ["A", "A", "B"]], ["name", ["Agent"]],
    ["audioRole", ["main"]], ["lyricOffset", ["12"]],
  ] }, "go");
  const reference = normalizeCanonical({ lines: [earlier, first, tied], metadata: [
    ["isrc", []], ["artists", ["A", "B"]], ["musicName", ["Synthetic"]],
  ] }, "reference");
  assert.deepEqual(go, reference);
  assert.deepEqual(go.lines.map((entry) => entry.words[0].text), ["earlier", "first", "tied"]);
});

test("authored whitespace, Unicode and reversed endpoints remain exact", () => {
  const original = { lines: [line("A ", 1000)], metadata: [] };
  original.lines[0].words[0].startMS = 2000;
  original.lines[0].words[0].endMS = 1000;
  original.lines[0].words[0].ruby = [{ text: "a\u0301", startMS: 3000, endMS: 500 }];
  const normalized = normalizeCanonical(original, "go");
  assert.equal(normalized.lines[0].words[0].text, "A ");
  assert.equal(normalized.lines[0].words[0].startMS, 2000);
  assert.equal(normalized.lines[0].words[0].endMS, 1000);
  assert.equal(normalized.lines[0].words[0].ruby[0].text, "a\u0301");
  const changed = structuredClone(normalized);
  changed.lines[0].words[0].text = "A";
  changed.lines[0].words[0].ruby[0].text = "á";
  const differences = [...fieldDifferences(normalized, changed)];
  assert.deepEqual(differences.map((entry) => entry.field), ["$.lines[0].words[0].ruby[0].text", "$.lines[0].words[0].text"]);
});

test("unknown nonempty metadata and altered value order fail comparison", () => {
  const expected = normalizeCanonical({ lines: [], metadata: [["artists", ["A", "B"]]] }, "reference");
  const actual = normalizeCanonical({ lines: [], metadata: [["artists", ["B", "A"]], ["unknown", ["value"]]] }, "go");
  assert.notDeepEqual(actual, expected);
  const differences = [...fieldDifferences(expected, actual)];
  assert.ok(differences.some((entry) => entry.field === "$.metadata.length"));
  assert.ok(differences.some((entry) => entry.field === "$.metadata[0][1][0]"));
  assert.throws(() => normalizeCanonical({ lines: [], metadata: [], skipped: true }, "go"), /outside the common schema/);
});

test("diff emits missing elements and each changed field without tolerance", () => {
  const expected = { lines: [line("A", 1000)] };
  const actual = structuredClone(expected);
  actual.lines[0].words[0].startMS++;
  actual.lines[0].translation = "translated";
  actual.lines.push(line("B", 3000));
  const fields = [...fieldDifferences(expected, actual)].map((entry) => entry.field);
  assert.deepEqual(fields, ["$.lines.length", "$.lines[0].translation", "$.lines[0].words[0].startMS", "$.lines[1]"]);
});

test("rejection parity requires both XML-stage failure and no partial Go output", () => {
  const rejected = { error: "synthetic unbound prefix", errorCode: "namespace", lineCount: 0, wordCount: 0, rubyCount: 0, metadataCount: 0 };
  assert.equal(rejectionParity(rejected, true), true);
  assert.equal(rejectionParity(rejected, false), false);
  for (const update of [
    { errorCode: "ttml" }, { errorCode: "time" }, { partialDocument: true },
    { canonical: { lines: [], metadata: [] } }, { snapshotSHA256: "hash" },
    { canonicalSHA256: "hash" }, { lineCount: 1 }, { wordCount: 1 }, { metadataCount: 1 },
  ]) {
    assert.equal(rejectionParity({ ...rejected, ...update }, true), false);
  }
});
