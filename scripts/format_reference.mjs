#!/usr/bin/env node
// Compile unchanged, pinned AMLL format sources only in a system temp folder.
import { execFileSync } from "node:child_process";
import { createHash } from "node:crypto";
import { createReadStream } from "node:fs";
import { mkdtemp, open, readFile, rm, writeFile } from "node:fs/promises";
import { createRequire } from "node:module";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { createInterface } from "node:readline";
import { pathToFileURL } from "node:url";
import { parseArgs } from "node:util";
import { fieldDifferences } from "./ttml_reference.mjs";

const CORPUS_COMMIT = "6007fcdf33bf838bea0304292992bbdc1d6e2bea";
const AMLL_COMMIT = "f32285864056587c4afa59d310208b0aa829cb53";
const EXTENSIONS = new Set([".lrc", ".eslrc", ".qrc", ".yrc"]);
const sha256 = (value) => createHash("sha256").update(value).digest("hex");
const lexical = (a, b) => a < b ? -1 : a > b ? 1 : 0;

function git(root, ...args) {
  return execFileSync("git", ["-C", root, ...args], { encoding: "utf8", maxBuffer: 128 << 20 });
}

function pinned(root, expected) {
  const actual = git(root, "rev-parse", "--verify", "HEAD").trim();
  if (actual !== expected) throw new Error(`${root}: commit differs from required ${expected}`);
  if (git(root, "status", "--porcelain", "--untracked-files=no").trim()) throw new Error(`${root}: tracked changes in reference checkout`);
  return actual;
}

function canonicalize(lines, format) {
  const result = lines.map((line) => ({
    startMS: line.startTime, endMS: line.endTime,
    words: line.words.map((word) => ({ text: word.word, startMS: word.startTime, endMS: word.endTime })),
    background: line.isBG,
  }));
  // QRC/YRC keep file order in AMLL; Go provides stable time-indexed documents.
  if (format === ".qrc" || format === ".yrc") result.sort((a, b) => a.startMS - b.startMS);
  return result;
}

async function main() {
  const { values } = parseArgs({ options: {
    corpus: { type: "string" }, amll: { type: "string" }, deps: { type: "string" },
    "go-records": { type: "string" }, report: { type: "string" }, differences: { type: "string" },
    "expect-corpus": { type: "string", default: CORPUS_COMMIT },
    "expect-amll": { type: "string", default: AMLL_COMMIT },
  } });
  for (const key of ["corpus", "amll", "deps", "go-records"]) if (!values[key]) throw new Error(`--${key} is required`);
  const corpus = resolve(values.corpus), amll = resolve(values.amll);
  const corpusCommit = pinned(corpus, values["expect-corpus"]);
  const referenceCommit = pinned(amll, values["expect-amll"]);
  const manifest = new Map();
  for (const item of git(corpus, "ls-files", "--stage", "-z").split("\0")) {
    if (!item) continue;
    const tab = item.indexOf("\t");
    const [mode, blob, stage] = item.slice(0, tab).split(" ");
    const path = item.slice(tab + 1), format = path.slice(path.lastIndexOf(".")).toLowerCase();
    if (!EXTENSIONS.has(format)) continue;
    if (tab < 0 || stage !== "0" || !["100644", "100755"].includes(mode)) throw new Error("invalid tracked corpus entry");
    manifest.set(path, { blob, format });
  }
  const require = createRequire(join(resolve(values.deps), "package.json"));
  const esbuild = require("esbuild");
  if (esbuild.version !== "0.25.12") throw new Error("esbuild@0.25.12 is required in --deps");
  const temporary = await mkdtemp(join(tmpdir(), "mygo-amll-format-reference-"));
  let differenceFile;
  try {
    const bundled = join(temporary, "reference.mjs");
    const modules = ["lrc/index.ts", "yrc.ts", "qrc.ts"].map((file) => JSON.stringify(join(amll, "packages/lyric/src/formats", file)));
    await esbuild.build({ stdin: { contents: `export { parseLrcLike } from ${modules[0]}; export { parseYrc } from ${modules[1]}; export { parseQrc } from ${modules[2]};`, resolveDir: amll }, bundle: true, platform: "node", format: "esm", target: "node22", outfile: bundled, logLevel: "silent" });
    const { parseLrcLike, parseYrc, parseQrc } = await import(pathToFileURL(bundled).href);
    if (values.differences) differenceFile = await open(values.differences, "w", 0o600);
    const emit = async (value) => {
      const row = `${JSON.stringify(value)}\n`;
      if (differenceFile) await differenceFile.writeFile(row); else process.stderr.write(row);
    };
    const summary = { corpusCommit, referenceCommit, nodeVersion: process.versions.node, esbuildVersion: esbuild.version,
      trackedPaths: manifest.size, checkedPaths: 0, formats: {}, evidenceErrors: 0, differingFields: 0 };
    const paths = new Set(), hashes = new Map();
    for await (const row of createInterface({ input: createReadStream(values["go-records"]), crlfDelay: Infinity })) {
      if (!row) continue;
      const record = JSON.parse(row), source = manifest.get(record.path);
      if (!source || paths.has(record.path) || source.format !== record.format) {
        summary.evidenceErrors++; await emit({ kind: "evidence", path: record.path, error: "extra/duplicate/invalid source path" }); continue;
      }
      paths.add(record.path); summary.checkedPaths++;
      const stat = summary.formats[source.format] ??= { paths: 0, unique: 0, matched: 0, different: 0, errors: 0 };
      stat.paths++;
      const bytes = await readFile(join(corpus, record.path));
      if (sha256(bytes) !== record.sourceSHA256) {
        summary.evidenceErrors++; await emit({ kind: "evidence", path: record.path, error: "source bytes differ from Go run" }); continue;
      }
      if (record.error) { stat.errors++; await emit({ kind: "go-error", path: record.path, error: record.error }); continue; }
      const key = `${source.format}:${source.blob}`;
      if (hashes.has(key)) {
        if (hashes.get(key) !== record.canonicalSHA256) { summary.evidenceErrors++; await emit({ kind: "evidence", path: record.path, error: "same Git blob produced a different Go result" }); }
        continue;
      }
      stat.unique++;
      if (!Array.isArray(record.canonical)) { summary.evidenceErrors++; await emit({ kind: "evidence", path: record.path, error: "first source record lacks canonical fields; use -canonical" }); continue; }
      try {
        const text = bytes.toString("utf8");
        const lines = source.format === ".qrc" ? parseQrc(text) : source.format === ".yrc" ? parseYrc(text) : parseLrcLike(text).lines;
        const expected = canonicalize(lines, source.format);
        const expectedJSON = JSON.stringify(expected).replaceAll("\u2028", "\\u2028").replaceAll("\u2029", "\\u2029");
        hashes.set(key, record.canonicalSHA256);
        // The fields are compared as values as well as checking the emitted hash.
        const differences = [...fieldDifferences(expected, record.canonical)];
        const emittedJSON = JSON.stringify(record.canonical).replaceAll("\u2028", "\\u2028").replaceAll("\u2029", "\\u2029");
        if (sha256(emittedJSON) !== record.canonicalSHA256) { summary.evidenceErrors++; await emit({ kind: "evidence", path: record.path, error: "canonical hash is not backed by its emitted fields" }); }
        if (differences.length === 0 && sha256(expectedJSON) === record.canonicalSHA256) stat.matched++;
        else {
          stat.different++; summary.differingFields += differences.length;
          for (const difference of differences) await emit({ kind: "field", path: record.path, blob: source.blob, ...difference });
        }
      } catch (error) { stat.errors++; await emit({ kind: "reference-error", path: record.path, error: String(error) }); }
    }
    for (const path of manifest.keys()) if (!paths.has(path)) { summary.evidenceErrors++; await emit({ kind: "evidence", path, error: "tracked path is missing from Go records" }); }
    const ordered = [...hashes].sort(([a], [b]) => lexical(a, b));
    summary.normalizedSHA256 = sha256(JSON.stringify(ordered));
    summary.passed = summary.checkedPaths === summary.trackedPaths && summary.evidenceErrors === 0 && Object.values(summary.formats).every((stat) => stat.different === 0 && stat.errors === 0 && stat.unique === stat.matched);
    const output = `${JSON.stringify(summary, null, 2)}\n`;
    if (values.report) await writeFile(values.report, output, { mode: 0o600 });
    process.stdout.write(output);
    if (!summary.passed) process.exitCode = 1;
  } finally {
    if (differenceFile) await differenceFile.close();
    await rm(temporary, { recursive: true, force: true });
  }
}

main().catch((error) => { console.error(error.stack ?? String(error)); process.exitCode = 1; });
