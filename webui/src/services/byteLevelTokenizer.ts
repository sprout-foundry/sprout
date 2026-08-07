/**
 * Byte-level BPE tokenizer for GPT-2 / Jina / RoBERTa-style tokenizers.
 *
 * This is the TypeScript counterpart to pkg/embedding/bytelevel_tokenizer.go.
 * The two MUST produce identical token IDs for the same input — the Go side
 * has a reference fixture (pkg/embedding/bytelevel_tokenizer_test.go) that
 * validates against the HuggingFace `tokenizers` library output, and the TS
 * implementation here is a direct port of the Go logic. When changing the
 * tokenization pipeline, update BOTH sides together.
 *
 * Unlike Gemma's SentencePiece tokenizer (which replaces spaces with ▁ and
 * merges on raw Unicode runes), ByteLevel maps every UTF-8 byte to a
 * printable Unicode code point via the fixed 256-entry GPT-2 bytes_to_unicode
 * table, pre-splits the input on whitespace and punctuation using the GPT-2
 * regex, then applies BPE on the byte-mapped strings. Space maps to 'Ġ'
 * (U+0120); '\n' maps to 'Ċ' (U+010A).
 *
 * Consumed by JinaCodeProvider (webui/src/services/onnxEmbeddingProvider.ts).
 */

// ─── Tokenizer schema ───────────────────────────────────────────
// Tightly scoped to what a ByteLevel BPE tokenizer ships: vocab + merges +
// added_tokens. The pre_tokenizer / decoder / post_processor fields are not
// needed for the embedding path.

export interface ByteLevelTokenizerJSON {
  model: {
    type: string;
    vocab: Record<string, number>;
    /**
     * HuggingFace ships merges in two formats:
     *   - newer: [["first", "second"], ...]
     *   - older: ["first second", ...]
     * We accept both; the older form is preserved so hand-written test
     * tokenizers stay readable.
     */
    merges: unknown;
  };
  added_tokens?: Array<{
    id: number;
    content: string;
    special: boolean;
  }>;
}

interface BpePair {
  first: string;
  second: string;
}

// ─── bytes_to_unicode table ─────────────────────────────────────
// Mirrors HuggingFace's bytes_to_unicode: printable bytes (33-126, 161-172,
// 174-255) map to themselves; all others shift up by 256 into a printable
// Unicode range. Space (32) maps to 'Ġ' (U+0120 = 288). Initialized from
// buildByteEncoder() — a direct port of buildByteEncoder in
// pkg/embedding/bytelevel_tokenizer.go.

function buildByteEncoder(): number[] {
  const printable = new Set<number>();
  for (let b = 33; b <= 126; b++) printable.add(b);
  for (let b = 161; b <= 172; b++) printable.add(b);
  for (let b = 174; b <= 255; b++) printable.add(b);

  const encoder = new Array<number>(256);
  let n = 0;
  for (let b = 0; b < 256; b++) {
    if (printable.has(b)) {
      encoder[b] = b;
    } else {
      encoder[b] = 256 + n;
      n++;
    }
  }
  return encoder;
}

const BYTE_ENCODER = buildByteEncoder();

const textEncoder = new TextEncoder();

/** Map each UTF-8 byte of s through bytes_to_unicode (spaces become 'Ġ', etc.). */
function byteEncodeString(s: string): string {
  const bytes = textEncoder.encode(s);
  let out = '';
  for (let i = 0; i < bytes.length; i++) {
    // All mapped code points are ≤ 417 (BMP), so fromCharCode is safe.
    out += String.fromCharCode(BYTE_ENCODER[bytes[i]]);
  }
  return out;
}

// ─── GPT-2 pre-tokenization regex helpers ───────────────────────
// Direct port of gpt2RegexSplit in pkg/embedding/bytelevel_tokenizer.go,
// which implements the GPT-2 pre-tokenization pattern:
//
//   's|'t|'re|'ve|'m|'ll|'d| ?\p{L}+| ?\p{N}+| ?[^\s\p{L}\p{N}]+|\s+(?!\S)|\s+
//
// The `\s+(?!\S)` alternative matches a whitespace run NOT followed by a
// non-whitespace char (i.e., trailing whitespace before end-of-input). This
// matters for multi-char whitespace sequences in code (\n\t\t\n etc.).

/** Go's unicode.IsSpace ≈ the Unicode White_Space property. */
function isSpace(ch: string): boolean {
  return /\p{White_Space}/u.test(ch);
}

/** Go's unicode.IsLetter ≈ \p{L}. */
function isLetter(ch: string): boolean {
  return /\p{L}/u.test(ch);
}

/** Go's unicode.IsNumber ≈ \p{N} (Nd, Nl, No). */
function isNumber(ch: string): boolean {
  return /\p{N}/u.test(ch);
}

/**
 * Split a string into GPT-2 pre-token segments. Operates on code points and
 * mirrors the Go implementation exactly, including the whitespace-run
 * splitting quirks (trailing whitespace before a non-space char is peeled
 * off so BPE can merge adjacent whitespace into vocab entries).
 */
function gpt2RegexSplit(s: string): string[] {
  const runes = Array.from(s); // code points
  const result: string[] = [];
  let i = 0;
  while (i < runes.length) {
    // Contraction: 's 't 're 've 'm 'll 'd
    if (runes[i] === "'" && i + 1 < runes.length) {
      let matched = false;
      for (const suf of ['s', 't', 're', 've', 'm', 'll', 'd']) {
        const sufRunes = Array.from(suf);
        if (i + 1 + sufRunes.length > runes.length) continue;
        let ok = true;
        for (let j = 0; j < sufRunes.length; j++) {
          if (runes[i + 1 + j] !== sufRunes[j]) {
            ok = false;
            break;
          }
        }
        if (ok) {
          result.push(runes.slice(i, i + 1 + sufRunes.length).join(''));
          i += 1 + sufRunes.length;
          matched = true;
          break;
        }
      }
      if (matched) continue;
    }

    const r = runes[i];

    // Non-space whitespace (\n, \t, etc.): HF's ByteLevel regex
    // \s+(?!\S)|\s+ splits whitespace runs. For a run of N whitespace chars
    // followed by non-whitespace: \s+(?!\S) matches N-1 chars, then \s+
    // matches the final char separately. This lets BPE merge adjacent
    // whitespace into vocab entries (e.g., \n+\t → Ċĉ).
    //
    // Space (32) is NOT handled here — it's consumed as an optional prefix
    // by the ` ?\p{L}+` etc. alternatives below, or falls through to the
    // standalone-space branch.
    if (isSpace(r) && r !== ' ') {
      const start = i;
      i++;
      while (i < runes.length && isSpace(runes[i]) && runes[i] !== ' ') {
        i++;
      }
      // If followed by non-whitespace (or end), peel off the last char.
      if (i > start + 1 && (i >= runes.length || !isSpace(runes[i]))) {
        result.push(runes.slice(start, i - 1).join(''));
        result.push(runes.slice(i - 1, i).join(''));
      } else {
        result.push(runes.slice(start, i).join(''));
      }
      continue;
    }

    // Standalone space run (not followed by a word/digit/punct token).
    // "  " (double space) → one segment, BPE merges Ġ+Ġ.
    // A single space before a word is consumed as a prefix below.
    if (r === ' ' && (i + 1 >= runes.length || runes[i + 1] === ' ')) {
      const start = i;
      i++;
      while (i < runes.length && runes[i] === ' ') {
        i++;
      }
      // Peel off last char only when followed by non-space whitespace
      // (e.g., "  \n"). At end-of-input, keep as one segment.
      if (i > start + 1 && i < runes.length && isSpace(runes[i]) && runes[i] !== ' ') {
        result.push(runes.slice(start, i - 1).join(''));
        result.push(runes.slice(i - 1, i).join(''));
      } else {
        result.push(runes.slice(start, i).join(''));
      }
      continue;
    }

    // Optional leading space before non-whitespace token.
    let spaceStart = i;
    if (r === ' ' && i + 1 < runes.length) {
      const next = runes[i + 1];
      if (!isSpace(next)) {
        i++;
      }
    }

    const ch = runes[i];
    const chIsLetter = isLetter(ch);
    const chIsNumber = isNumber(ch);

    if (chIsLetter) {
      i++;
      while (i < runes.length && isLetter(runes[i])) {
        i++;
      }
      result.push(runes.slice(spaceStart, i).join(''));
    } else if (chIsNumber) {
      i++;
      while (i < runes.length && isNumber(runes[i])) {
        i++;
      }
      result.push(runes.slice(spaceStart, i).join(''));
    } else {
      i++;
      while (
        i < runes.length &&
        !isLetter(runes[i]) &&
        !isNumber(runes[i]) &&
        !isSpace(runes[i])
      ) {
        i++;
      }
      result.push(runes.slice(spaceStart, i).join(''));
    }
  }
  return result;
}

/** Build a merge-table key. Uses U+001F (unit separator) to avoid collisions
 *  with any legitimate codepoint that could appear in vocabulary strings. */
function mergeKey(a: string, b: string): string {
  return a + '\x1f' + b;
}

/** Accepts either pair-array (`[["a","b"]]`) or joined-string (`["a b"]`) merge formats. */
function parseMerges(raw: unknown): BpePair[] {
  if (raw == null) return [];
  if (!Array.isArray(raw)) throw new Error('tokenizer: merges must be an array');
  const out: BpePair[] = [];
  for (let i = 0; i < raw.length; i++) {
    const entry = raw[i];
    if (Array.isArray(entry)) {
      if (entry.length !== 2 || typeof entry[0] !== 'string' || typeof entry[1] !== 'string') {
        throw new Error(`tokenizer: merges[${i}] must be a 2-string array`);
      }
      out.push({ first: entry[0], second: entry[1] });
    } else if (typeof entry === 'string') {
      const idx = entry.indexOf(' ');
      if (idx < 0) throw new Error(`tokenizer: merges[${i}] "${entry}" has no space separator`);
      out.push({ first: entry.slice(0, idx), second: entry.slice(idx + 1) });
    } else {
      throw new Error(`tokenizer: merges[${i}] unrecognized type`);
    }
  }
  return out;
}

/**
 * Split a string into single-codepoint pieces. JavaScript's `for...of`
 * iterates code points (not UTF-16 code units), which correctly handles
 * astral-plane characters.
 */
function splitIntoCodepoints(s: string): string[] {
  const out: string[] = [];
  for (const cp of s) out.push(cp);
  return out;
}

/**
 * ByteLevelTokenizer tokenizes text using the HuggingFace ByteLevel BPE
 * pipeline (GPT-2 / Jina / RoBERTa family).
 *
 * Pipeline (matches the Go reference in pkg/embedding/bytelevel_tokenizer.go):
 *
 *   1. Pre-tokenize with the GPT-2 regex (contractions, letters, numbers,
 *      punctuation, whitespace runs).
 *   2. Map each segment's UTF-8 bytes through the bytes_to_unicode table.
 *   3. Apply rank-ordered BPE merges to each byte-mapped segment.
 *   4. Map each resulting symbol to its vocab id, falling back to <unk> on miss.
 *
 * No special tokens are added by Encode(); use encodeWithBOSAndEOS() for the
 * wrapped form.
 */
export class ByteLevelTokenizer {
  private readonly vocab: Map<string, number>;
  private readonly bpeRanks: Map<string, number>; // key = first + "\x1f" + second

  readonly bosID: number;
  readonly eosID: number;
  readonly unkID: number;
  readonly padID: number;
  readonly vocabSize: number;

  constructor(config: ByteLevelTokenizerJSON) {
    const model = config.model;
    if (model.type !== 'BPE') {
      throw new Error(`Expected BPE tokenizer, got ${model.type}`);
    }

    this.vocab = new Map(Object.entries(model.vocab));
    this.vocabSize = this.vocab.size;

    this.bpeRanks = new Map();
    const merges = parseMerges(model.merges);
    for (let i = 0; i < merges.length; i++) {
      this.bpeRanks.set(mergeKey(merges[i].first, merges[i].second), i);
    }

    // Resolve the special token IDs by content, mirroring the Go side:
    // <s>/<bos> → BOS, </s>/<eos> → EOS, <unk> → UNK, <pad> → PAD.
    let bos = -1,
      eos = -1,
      unk = -1,
      pad = -1;
    for (const at of config.added_tokens ?? []) {
      if (!at.content) continue;
      switch (at.content) {
        case '<s>':
        case '<bos>':
          bos = at.id;
          break;
        case '</s>':
        case '<eos>':
          eos = at.id;
          break;
        case '<unk>':
          unk = at.id;
          break;
        case '<pad>':
          pad = at.id;
          break;
      }
    }
    this.bosID = bos;
    this.eosID = eos;
    this.unkID = unk;
    this.padID = pad;
  }

  /**
   * Tokenize text to a sequence of token IDs. Does NOT add BOS/EOS — use
   * encodeWithBOSAndEOS() for the wrapped form. Empty input returns [].
   */
  tokenize(text: string): number[] {
    if (text.length === 0) return [];
    const out: number[] = [];
    for (const word of gpt2RegexSplit(text)) {
      const mapped = byteEncodeString(word);
      // Direct vocab hit on the byte-encoded word (mirrors the Go Encode:
      // this is rarely hit for ByteLevel models since pre-tokenized words
      // are usually multi-symbol, but it must be checked first).
      const id = this.vocab.get(mapped);
      if (id !== undefined) {
        out.push(id);
        continue;
      }
      this.bpeEncodeWord(mapped, out);
    }
    return out;
  }

  /**
   * Encode with BOS prepended and EOS appended, mirroring
   * ByteLevelTokenizer.EncodeWithBOSAndEOS on the Go side.
   */
  encodeWithBOSAndEOS(text: string): number[] {
    const bare = this.tokenize(text);
    const out: number[] = [];
    if (this.bosID >= 0) out.push(this.bosID);
    for (const id of bare) out.push(id);
    if (this.eosID >= 0) out.push(this.eosID);
    return out;
  }

  private bpeEncodeWord(word: string, out: number[]): void {
    if (word.length === 0) return;
    const symbols = splitIntoCodepoints(word);
    const merged = this.applyBPE(symbols);
    for (const sym of merged) {
      const id = this.vocab.get(sym);
      if (id !== undefined) {
        out.push(id);
      } else if (this.unkID >= 0) {
        out.push(this.unkID);
      }
    }
  }

  /**
   * Classical BPE: repeatedly merge the pair with the lowest merge rank
   * until no merge applies. Operates on a slice of single-codepoint symbol
   * strings that grow as merges are applied. All occurrences of the chosen
   * pair are merged in a single pass, matching HF's behavior so the output
   * stays byte-identical to its reference. Direct port of applyBPEMerge in
   * pkg/embedding/bytelevel_tokenizer.go.
   */
  private applyBPE(symbols: string[]): string[] {
    if (symbols.length < 2) return symbols;
    for (;;) {
      let bestRank = Number.MAX_SAFE_INTEGER;
      let bestIdx = -1;
      for (let i = 0; i < symbols.length - 1; i++) {
        const rank = this.bpeRanks.get(mergeKey(symbols[i], symbols[i + 1]));
        if (rank !== undefined && rank < bestRank) {
          bestRank = rank;
          bestIdx = i;
        }
      }
      if (bestIdx < 0) return symbols;

      const first = symbols[bestIdx];
      const second = symbols[bestIdx + 1];
      const merged: string[] = [];
      for (let i = 0; i < symbols.length; ) {
        if (i + 1 < symbols.length && symbols[i] === first && symbols[i + 1] === second) {
          merged.push(symbols[i] + symbols[i + 1]);
          i += 2;
        } else {
          merged.push(symbols[i]);
          i++;
        }
      }
      symbols = merged;
    }
  }
}
