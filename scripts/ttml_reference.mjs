#!/usr/bin/env node
// Offline comparison with an unchanged, pinned AMLL TTMLParser/toAmllLyrics.
// Dependencies are resolved only from an explicitly supplied temporary folder.
import { execFileSync } from "node:child_process";
import { createHash } from "node:crypto";
import { createReadStream } from "node:fs";
import { mkdtemp, open, readFile, rm, writeFile } from "node:fs/promises";
import { createRequire } from "node:module";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { createInterface } from "node:readline";
import { fileURLToPath, pathToFileURL } from "node:url";
import { parseArgs } from "node:util";

const CORPUS_COMMIT = "6007fcdf33bf838bea0304292992bbdc1d6e2bea";
const AMLL_COMMIT = "f32285864056587c4afa59d310208b0aa829cb53";
const XMLDOM_VERSION = "0.9.10";
const ESBUILD_VERSION = "0.25.12";
const GO_METADATA_EXTENSIONS = new Set(["name", "lyricOffset", "audioRole"]);
const XML_REJECTION_CODES = new Set(["xml", "namespace"]);
const lexical = (left, right) => (left < right ? -1 : left > right ? 1 : 0);
const sha256 = (value) => createHash("sha256").update(value).digest("hex");

function object(value, where, keys) {
  if (value === null || typeof value !== "object" || Array.isArray(value)) {
    throw new Error(`${where} must be an object`);
  }
  for (const key of Object.keys(value)) {
    if (!keys.includes(key)) throw new Error(`${where}.${key} is outside the common schema`);
  }
}

function array(value, where) {
  if (!Array.isArray(value)) throw new Error(`${where} must be an array`);
  return value;
}

function text(value, where) {
  if (typeof value !== "string") throw new Error(`${where} must be a string`);
  return value;
}

function number(value, where) {
  if (typeof value !== "number" || !Number.isFinite(value)) {
    throw new Error(`${where} must be a finite number`);
  }
  return value;
}

function boolean(value, where) {
  if (typeof value !== "boolean") throw new Error(`${where} must be a boolean`);
  return value;
}

// The only normalization rules are stable line-start ordering and metadata
// key grouping/order. Strings and authored time endpoints are unchanged.
export function normalizeCanonical(input, side) {
  if (side !== "go" && side !== "reference") throw new Error("unknown comparison side");
  object(input, "$", ["lines", "metadata"]);
  const lines = array(input.lines, "$.lines").map((line, index) => {
    const where = `$.lines[${index}]`;
    object(line, where, ["words", "translation", "romanization", "startMS", "endMS", "background", "duet"]);
    return {
      words: array(line.words, `${where}.words`).map((word, wordIndex) => {
        const wordAt = `${where}.words[${wordIndex}]`;
        object(word, wordAt, ["text", "romanization", "startMS", "endMS", "obscene", "emptyBeat", "ruby"]);
        return {
          text: text(word.text, `${wordAt}.text`),
          romanization: text(word.romanization, `${wordAt}.romanization`),
          startMS: number(word.startMS, `${wordAt}.startMS`),
          endMS: number(word.endMS, `${wordAt}.endMS`),
          obscene: boolean(word.obscene, `${wordAt}.obscene`),
          emptyBeat: number(word.emptyBeat, `${wordAt}.emptyBeat`),
          ruby: array(word.ruby, `${wordAt}.ruby`).map((ruby, rubyIndex) => {
            const rubyAt = `${wordAt}.ruby[${rubyIndex}]`;
            object(ruby, rubyAt, ["text", "startMS", "endMS"]);
            return {
              text: text(ruby.text, `${rubyAt}.text`),
              startMS: number(ruby.startMS, `${rubyAt}.startMS`),
              endMS: number(ruby.endMS, `${rubyAt}.endMS`),
            };
          }),
        };
      }),
      translation: text(line.translation, `${where}.translation`),
      romanization: text(line.romanization, `${where}.romanization`),
      startMS: number(line.startMS, `${where}.startMS`),
      endMS: number(line.endMS, `${where}.endMS`),
      background: boolean(line.background, `${where}.background`),
      duet: boolean(line.duet, `${where}.duet`),
    };
  });
  lines.sort((left, right) => left.startMS - right.startMS);
  const grouped = new Map();
  for (const [index, entry] of array(input.metadata, "$.metadata").entries()) {
    array(entry, `$.metadata[${index}]`);
    if (entry.length !== 2) throw new Error(`$.metadata[${index}] must be a key/value pair`);
    let name = text(entry[0], `$.metadata[${index}][0]`);
    const values = array(entry[1], `$.metadata[${index}][1]`).map((value) => text(value, `$.metadata[${index}][1]`));
    if (side === "go" && GO_METADATA_EXTENSIONS.has(name)) continue;
    if (name === "title") name = "musicName";
    if (values.length === 0) continue;
    if (!grouped.has(name)) grouped.set(name, []);
    const target = grouped.get(name);
    for (const value of values) if (!target.includes(value)) target.push(value);
  }
  const metadata = [...grouped].sort(([left], [right]) => lexical(left, right));
  return { lines, metadata };
}

// Every differing scalar/array slot is emitted; there are no input-specific
// exemptions, similarity tolerances, trimming or Unicode normalization rules.
export function* fieldDifferences(expected, actual, path = "$") {
  if (Object.is(expected, actual)) return;
  if (Array.isArray(expected) && Array.isArray(actual)) {
    if (expected.length !== actual.length) {
      yield { field: `${path}.length`, expected: expected.length, actual: actual.length };
    }
    for (let index = 0; index < Math.max(expected.length, actual.length); index++) {
      if (index >= expected.length || index >= actual.length) {
        yield { field: `${path}[${index}]`, expected: index < expected.length ? expected[index] : { missing: true }, actual: index < actual.length ? actual[index] : { missing: true } };
      } else {
        yield* fieldDifferences(expected[index], actual[index], `${path}[${index}]`);
      }
    }
    return;
  }
  if (expected !== null && actual !== null && typeof expected === "object" && typeof actual === "object" && !Array.isArray(expected) && !Array.isArray(actual)) {
    const keys = [...new Set([...Object.keys(expected), ...Object.keys(actual)])].sort(lexical);
    for (const key of keys) {
      if (!Object.hasOwn(expected, key) || !Object.hasOwn(actual, key)) {
        yield { field: `${path}.${key}`, expected: Object.hasOwn(expected, key) ? expected[key] : { missing: true }, actual: Object.hasOwn(actual, key) ? actual[key] : { missing: true } };
      } else {
        yield* fieldDifferences(expected[key], actual[key], `${path}.${key}`);
      }
    }
    return;
  }
  yield { field: path, expected, actual };
}

// Rejection parity requires a structural/namespace diagnostic, no partial Go
// output, and an independently observed failure inside reference DOM parsing.
export function rejectionParity(record, referenceXMLRejected) {
  return referenceXMLRejected && Boolean(record.error) && XML_REJECTION_CODES.has(record.errorCode) &&
    !record.partialDocument && record.canonical == null && !record.snapshotSHA256 && !record.canonicalSHA256 &&
    record.lineCount === 0 && record.wordCount === 0 && record.rubyCount === 0 && record.metadataCount === 0;
}

function git(root, ...args) {
  return execFileSync("git", ["-C", root, ...args], { encoding: "utf8", maxBuffer: 128 << 20, stdio: ["ignore", "pipe", "pipe"] });
}

function pinnedRevision(root, expected) {
  const commit = git(root, "rev-parse", "--verify", "HEAD").trim();
  if (commit !== expected) throw new Error(`${root}: HEAD ${commit} differs from required ${expected}`);
  if (git(root, "status", "--porcelain", "--untracked-files=no").trim()) {
    throw new Error(`${root}: reference checkout has tracked changes`);
  }
  return commit;
}

function manifest(root) {
  const files = new Map();
  for (const record of git(root, "ls-files", "--stage", "-z").split("\0")) {
    if (!record) continue;
    const tab = record.indexOf("\t");
    const [mode, blob, stage] = record.slice(0, tab).split(" ");
    const path = record.slice(tab + 1);
    if (tab < 0 || stage !== "0") throw new Error("invalid/conflicted Git index record");
    if (!path.toLowerCase().endsWith(".ttml")) continue;
    if (mode !== "100644" && mode !== "100755") throw new Error(`${path}: TTML is not a regular tracked file`);
    files.set(path, blob);
  }
  if (files.size === 0) throw new Error("no tracked TTML paths found");
  return files;
}

async function dependencyVersion(require, name) {
  let path = dirname(require.resolve(name));
  for (;;) {
    try {
      const value = JSON.parse(await readFile(join(path, "package.json"), "utf8"));
      if (value.name === name) return value.version;
    } catch (error) {
      if (error.code !== "ENOENT") throw error;
    }
    const parent = dirname(path);
    if (parent === path) throw new Error(`cannot identify installed ${name} version`);
    path = parent;
  }
}

function referenceCanonical(result) {
  return {
    lines: result.lines.map((line) => ({
      words: line.words.map((word) => ({
        text: word.word,
        romanization: word.romanWord ?? "",
        startMS: word.startTime,
        endMS: word.endTime,
        obscene: word.obscene ?? false,
        emptyBeat: word.emptyBeat ?? 0,
        ruby: (word.ruby ?? []).map((ruby) => ({ text: ruby.word, startMS: ruby.startTime, endMS: ruby.endTime })),
      })),
      translation: line.translatedLyric,
      romanization: line.romanLyric,
      startMS: line.startTime,
      endMS: line.endTime,
      background: line.isBG,
      duet: line.isDuet,
    })),
    metadata: result.metadata.map(([name, values]) => [name, values]),
  };
}

async function main() {
  const { values } = parseArgs({ options: {
    corpus: { type: "string" }, amll: { type: "string" }, deps: { type: "string" },
    "go-records": { type: "string" }, report: { type: "string" }, differences: { type: "string" },
    "expect-corpus": { type: "string", default: CORPUS_COMMIT },
    "expect-amll": { type: "string", default: AMLL_COMMIT },
  } });
  for (const key of ["corpus", "amll", "deps", "go-records"]) {
    if (!values[key]) throw new Error(`--${key} is required; see scripts/ttml-reference.md`);
  }
  const corpus = resolve(values.corpus);
  const amll = resolve(values.amll);
  const dependencyRoot = resolve(values.deps);
  const sourceCommit = pinnedRevision(corpus, values["expect-corpus"]);
  const referenceCommit = pinnedRevision(amll, values["expect-amll"]);
  const files = manifest(corpus);
  const require = createRequire(join(dependencyRoot, "package.json"));
  const xmldomVersion = await dependencyVersion(require, "@xmldom/xmldom");
  const esbuildVersion = await dependencyVersion(require, "esbuild");
  if (xmldomVersion !== XMLDOM_VERSION || esbuildVersion !== ESBUILD_VERSION) {
    throw new Error(`required @xmldom/xmldom@${XMLDOM_VERSION} and esbuild@${ESBUILD_VERSION}; found ${xmldomVersion}/${esbuildVersion}`);
  }
  const { DOMParser } = require("@xmldom/xmldom");
  const esbuild = require("esbuild");
  const temporary = await mkdtemp(join(tmpdir(), "mygo-amll-ttml-reference-"));
  let differenceFile;
  try {
    const bundled = join(temporary, "reference.mjs");
    await esbuild.build({ entryPoints: [join(amll, "packages/ttml/src/index.ts")], bundle: true,
      format: "esm", platform: "node", target: "node22", outfile: bundled, logLevel: "silent" });
    const { TTMLParser, toAmllLyrics } = await import(pathToFileURL(bundled).href);
    if (values.differences) differenceFile = await open(values.differences, "w", 0o600);
    const summary = {
      normalization: "amll-simplified-v1",
      corpusCommit: sourceCommit, referenceCommit, xmldomVersion, esbuildVersion,
      nodeVersion: process.versions.node, trackedPaths: files.size,
      uniqueBlobs: new Set(files.values()).size, checkedPaths: 0, checkedUniqueBlobs: 0,
      matchedUniqueBlobs: 0, goFailedPaths: 0, referenceFailedBlobs: 0,
      validReferenceBlobs: 0, rejectedInvalidBlobs: 0, rejectedInvalidPaths: 0,
      differingUniqueBlobs: 0, differenceFields: 0, evidenceErrors: 0,
    };
    const emit = async (value) => {
      const json = `${JSON.stringify(value)}\n`;
      if (differenceFile) await differenceFile.writeFile(json);
      else process.stderr.write(json);
    };
    const badEvidence = async (path, error) => {
      summary.evidenceErrors++;
      await emit({ kind: "evidence-error", path, error });
    };
    const paths = new Set();
    const references = new Map();
    const records = createInterface({ input: createReadStream(values["go-records"]), crlfDelay: Infinity });
    for await (const json of records) {
      if (!json.trim()) continue;
      const record = JSON.parse(json);
      const path = record.path;
      if (typeof path !== "string" || !files.has(path) || paths.has(path)) {
        await badEvidence(path, "extra, duplicate or invalid path in Go records");
        continue;
      }
      paths.add(path);
      summary.checkedPaths++;
      if (record.blob !== files.get(path)) {
        await badEvidence(path, "Go record blob differs from the pinned corpus index");
        continue;
      }
      let reference = references.get(record.blob);
      if (!reference) {
        summary.checkedUniqueBlobs++;
        reference = { failed: false, goHash: undefined };
        references.set(record.blob, reference);
        let source;
        let expected;
        try {
          source = await readFile(join(corpus, path));
          if (sha256(source) !== record.sourceSHA256) throw new Error("Go record source SHA256 differs from current checkout bytes");
        } catch (error) {
          reference.failed = true;
          await badEvidence(path, error?.message ?? String(error));
        }
        if (!reference.failed) {
          let xmlRejected = false;
          try {
            const domParser = new DOMParser();
            const observedDOMParser = {
              parseFromString(...args) {
                try {
                  const document = domParser.parseFromString(...args);
                  if (document.getElementsByTagName("parsererror").length !== 0) xmlRejected = true;
                  return document;
                } catch (error) {
                  xmlRejected = true;
                  throw error;
                }
              },
            };
            const parser = new TTMLParser({ domParser: observedDOMParser });
            expected = normalizeCanonical(referenceCanonical(toAmllLyrics(parser.parse(source.toString("utf8")))), "reference");
            reference.referenceHash = sha256(JSON.stringify(expected));
            summary.validReferenceBlobs++;
          } catch (error) {
            if (rejectionParity(record, xmlRejected)) {
              reference.rejected = true;
              summary.rejectedInvalidBlobs++;
              await emit({ kind: "rejection-parity", path, blob: record.blob, goCode: record.errorCode,
                goError: record.error, referenceError: error?.message ?? String(error) });
            } else {
              reference.failed = true;
              summary.referenceFailedBlobs++;
              await emit({ kind: "reference-error", path, blob: record.blob, xmlRejected, error: error?.message ?? String(error) });
            }
          }
        }
        if (!reference.failed && !reference.rejected && !record.error) {
          try {
            if (!record.canonical) throw new Error("Go records require -canonical");
            const actual = normalizeCanonical(record.canonical, "go");
            reference.goHash = sha256(JSON.stringify(actual));
            if (reference.goHash === reference.referenceHash) {
              summary.matchedUniqueBlobs++;
            } else {
              summary.differingUniqueBlobs++;
              for (const difference of fieldDifferences(expected, actual)) {
                summary.differenceFields++;
                await emit({ kind: "field-difference", path, blob: record.blob, ...difference });
              }
            }
          } catch (error) {
            reference.failed = true;
            await badEvidence(path, error?.message ?? String(error));
          }
        }
      } else if (reference.rejected && !record.error) {
        await badEvidence(path, "a copy of an invalid rejected Git blob unexpectedly produced a Go document");
      } else if (!record.error && !reference.failed && reference.goHash !== undefined) {
        try {
          if (!record.canonical) throw new Error("Go records require -canonical");
          const hash = sha256(JSON.stringify(normalizeCanonical(record.canonical, "go")));
          if (hash !== reference.goHash) throw new Error("identical Git blobs produced different Go canonical output");
        } catch (error) {
          await badEvidence(path, error?.message ?? String(error));
        }
      }
      if (record.error) {
        if (reference.rejected && rejectionParity(record, true)) {
          summary.rejectedInvalidPaths++;
        } else {
          summary.goFailedPaths++;
          await emit({ kind: "go-error", path, blob: record.blob, error: record.error });
        }
      }
    }
    for (const path of [...files.keys()].sort(lexical)) {
      if (!paths.has(path)) await badEvidence(path, "tracked path missing from Go records");
    }
    const ordered = [...references].sort(([left], [right]) => lexical(left, right));
    summary.normalizedReferenceSHA256 = sha256(JSON.stringify(ordered.map(([blob, value]) => [blob, value.referenceHash ?? null])));
    summary.normalizedGoSHA256 = sha256(JSON.stringify(ordered.map(([blob, value]) => [blob, value.goHash ?? null])));
    summary.passed = summary.goFailedPaths === 0 && summary.referenceFailedBlobs === 0 &&
      summary.differingUniqueBlobs === 0 && summary.evidenceErrors === 0 && summary.checkedPaths === summary.trackedPaths &&
      summary.checkedUniqueBlobs === summary.uniqueBlobs && summary.matchedUniqueBlobs + summary.rejectedInvalidBlobs === summary.uniqueBlobs;
    const output = `${JSON.stringify(summary, null, 2)}\n`;
    if (values.report) await writeFile(values.report, output, { mode: 0o600 });
    process.stdout.write(output);
    if (!summary.passed) process.exitCode = 1;
  } finally {
    if (differenceFile) await differenceFile.close();
    await rm(temporary, { recursive: true, force: true });
  }
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  main().catch((error) => {
    console.error(error.stack ?? error.message ?? String(error));
    process.exitCode = 1;
  });
}
