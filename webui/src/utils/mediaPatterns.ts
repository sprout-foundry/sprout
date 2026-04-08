// File-type classification by extension — O(1) lookups via pre-built Sets.

const IMAGE_EXT = new Set([
  'png','jpg','jpeg','gif','bmp','webp','ico','tiff','tif','avif',
]);

const AUDIO_EXT = new Set([
  'mp3','wav','ogg','flac','aac','m4a','wma','opus','weba','mid','midi','mp4a',
]);

const VIDEO_EXT = new Set([
  'mp4','webm','mov','avi','mkv','m4v','flv','wmv','ogv','3gp',
]);

const BINARY_EXT = new Set([
  // compressed
  'zip','tar','gz','bz2','xz','7z','rar','zst','tgz',
  // executables / raw binary
  'exe','dll','so','dylib','bin','app','dat',
  // data / docs
  'pdf','doc','docx','xls','xlsx','ppt','pptx','odt','ods',
  // databases
  'db','sqlite','sqlite3',
  // fonts
  'woff','woff2','ttf','otf','eot',
  // compiled
  'class','o','obj','pyc','pyo','wasm',
  // packages
  'iso','dmg','apk','deb','rpm','jar','war',
  // serialized
  'pkl','pickle','parquet','arrow',
]);

const TEXT_EXT = new Set([
  'txt','md','json','xml','html','css','js','ts','tsx','jsx',
  'go','py','rs','java','c','h','cpp','hpp','sh','bash','zsh','fish',
  'yml','yaml','toml','ini','cfg','conf','env','gitignore','dockerfile',
  'makefile','cmake','gradle','sql','r','rb','php','pl','lua','vim','el',
  'clj','hs','ml','ex','exs','erl','swift','kt','scala','dart',
  'vue','svelte','astro','graphql','proto','grpc','tf','hcl',
  'mod','sum','log','csv','tsv','svg','rst','adoc','tex','org',
]);

function norm(ext?: string): string | undefined {
  if (!ext) return undefined;
  return ext.startsWith('.') ? ext.slice(1).toLowerCase() : ext.toLowerCase();
}

export function isImageFile(ext?: string): boolean {
  const e = norm(ext);
  return e !== undefined && IMAGE_EXT.has(e);
}

export function isAudioFile(ext?: string): boolean {
  const e = norm(ext);
  return e !== undefined && AUDIO_EXT.has(e);
}

export function isVideoFile(ext?: string): boolean {
  const e = norm(ext);
  return e !== undefined && VIDEO_EXT.has(e);
}

export function isMediaFile(ext?: string): boolean {
  return isImageFile(ext) || isAudioFile(ext) || isVideoFile(ext);
}

export function isBinaryFile(ext?: string): boolean {
  const e = norm(ext);
  return e !== undefined && BINARY_EXT.has(e);
}

export function isTextFile(ext?: string): boolean {
  const e = norm(ext);
  return e !== undefined && TEXT_EXT.has(e);
}

export function getMediaCategory(ext?: string): 'image' | 'audio' | 'video' | null {
  if (isImageFile(ext)) return 'image';
  if (isAudioFile(ext)) return 'audio';
  if (isVideoFile(ext)) return 'video';
  return null;
}
