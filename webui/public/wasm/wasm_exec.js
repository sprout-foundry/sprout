// Copyright 2018 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// This is a browser-compatible version of Go's wasm_exec.js (js/wasm target).
//
// It is derived from the upstream $(go env GOROOT)/lib/wasm/wasm_exec.js but
// replaces the no-op globalThis.fs shim with a real in-memory filesystem so
// that Go's syscall layer (syscall/fs_js.go) can read, write, stat and list
// files when compiled with GOOS=js GOARCH=wasm and executed in the browser.
//
// The rest of the Go runtime support (the globalThis.Go class, importObject,
// run(), and the wasm import trampolines) is copied verbatim from the
// standard library.

"use strict";

(() => {
	// ---------------------------------------------------------------------------
	// In-memory filesystem
	// ---------------------------------------------------------------------------
	//
	// Files are stored as a flat Map<string, Uint8Array> keyed by absolute path.
	// Directories are implicit: a path is considered a directory if it is the
	// prefix of any stored file (e.g. "/foo" is a dir while "/foo/bar.txt" exists)
	// or if it has been explicitly created with mkdir.
	//
	// The API mirrors the subset of Node.js's fs module that Go's
	// syscall/fs_js.go actually calls: open/read/write/close/fstat/stat/lstat/
	// readdir/mkdir/unlink/rmdir/rename plus writeSync for console output. All
	// methods use the Node.js (err, value) callback convention so they slot
	// directly into Go's fsCall wrapper.

	if (!globalThis.fs) {
		const encoder = new TextEncoder(); // utf-8 by default
		const decoder = new TextDecoder(); // utf-8 by default

		// --- filesystem state --------------------------------------------------

		// path -> Uint8Array for regular files.
		const files = new Map();

		// Set of paths that are explicitly directories (created via mkdir). This
		// lets empty directories exist even when no files live beneath them.
		const dirs = new Set();
		dirs.add("/");

		// path -> mtime (ms since epoch) for every entry (file or dir).
		const mtimes = new Map();

		// file descriptors: number -> { path, pos }
		// fds 0/1/2 are reserved for stdin/stdout/stderr and are never stored here.
		const openFiles = new Map();
		let nextFD = 3;

		// --- helpers -----------------------------------------------------------

		const now = () => Date.now();

		const normalize = (p) => {
			// Collapse "."/".." segments and repeated slashes. Every path is treated
			// as absolute (relative to "/"); a leading slash is added if missing.
			const isAbs = p.length > 0 && p[0] === "/";
			const parts = p.split("/");
			const stack = [];
			for (const part of parts) {
				if (part === "" || part === ".") {
					continue;
				}
				if (part === "..") {
					if (stack.length > 0) {
						stack.pop();
					}
					continue;
				}
				stack.push(part);
			}
			let result = stack.join("/");
			if (isAbs || result === "") {
				result = "/" + result;
			}
			return result;
		};

		// A path is an "implicit directory" if some stored file lives beneath it.
		const isImplicitDir = (p) => {
			p = normalize(p);
			const prefix = p === "/" ? "/" : p + "/";
			for (const key of files.keys()) {
				if (key.startsWith(prefix)) {
					return true;
				}
			}
			return false;
		};

		// A directory "has children" if any file or explicit subdirectory lives
		// directly beneath it. Used to decide whether rmdir should fail with
		// ENOTEMPTY. (isImplicitDir only considers files, so a dir that contains
		// nothing but empty subdirectories would otherwise look empty.)
		const hasChildren = (p) => {
			p = normalize(p);
			const prefix = p === "/" ? "/" : p + "/";
			for (const key of files.keys()) {
				if (key.startsWith(prefix)) {
					return true;
				}
			}
			for (const d of dirs) {
				if (d.startsWith(prefix)) {
					return true;
				}
			}
			return false;
		};

		const isDir = (p) => {
			p = normalize(p);
			if (p === "/" || dirs.has(p)) {
				return true;
			}
			return isImplicitDir(p);
		};

		// Generate a unique, stable-ish inode number for a path. Two calls with the
		// same path produce the same number, which is helpful for callers (e.g.
		// os.SameFile) that compare inodes.
		const inodeFor = (p) => {
			let h = 5381;
			for (let i = 0; i < p.length; i++) {
				h = ((h << 5) + h + p.charCodeAt(i)) | 0;
			}
			// Keep it positive and non-zero.
			return (h >>> 0) || 1;
		};

		// Build a Node.js-compatible stats object. The shape must satisfy Go's
		// syscall.setStat (syscall/fs_js.go), which reads dev, ino, mode, nlink,
		// uid, gid, rdev, size, blksize, blocks, atimeMs, mtimeMs and ctimeMs.
		// os.Open also calls stat.isDirectory() to decide whether to read entries.
		const S_IFREG = 0o100000; // regular file
		const S_IFDIR = 0o040000; // directory

		const makeStats = (pathStr, isDirectory, size) => {
			const mode = isDirectory ? (S_IFDIR | 0o777) : (S_IFREG | 0o666);
			const mtimeMs = mtimes.get(normalize(pathStr)) || now();
			// Record mtime so subsequent stats are stable.
			mtimes.set(normalize(pathStr), mtimeMs);
			const blocks = Math.max(1, Math.ceil(size / 512));
			return {
				dev: 0,
				ino: inodeFor(normalize(pathStr)),
				mode: mode,
				nlink: 1,
				uid: 0,
				gid: 0,
				rdev: 0,
				size: size,
				blksize: 4096,
				blocks: blocks,
				atimeMs: mtimeMs,
				mtimeMs: mtimeMs,
				ctimeMs: mtimeMs,
				birthtimeMs: mtimeMs,
				isDirectory: () => isDirectory,
				isFile: () => !isDirectory,
				isBlockDevice: () => false,
				isCharacterDevice: () => false,
				isFIFO: () => false,
				isSymbolicLink: () => false,
				isSocket: () => false,
			};
		};

		// Create an Error carrying a Node.js-style error code. Go's mapJSError
		// (syscall/fs_js.go) looks up err.code in its errnoByCode table.
		const errWithCode = (code, message) => {
			const err = new Error(message || code);
			err.code = code;
			return err;
		};

		const ENOENT = (p) => errWithCode("ENOENT", p === undefined ? "no such file or directory" : `no such file or directory, ${p}`);
		const EISDIR = () => errWithCode("EISDIR", "illegal operation on a directory, read");
		const ENOTDIR = () => errWithCode("ENOTDIR", "not a directory");
		const EEXIST = () => errWithCode("EEXIST", "file already exists");
		const EBADF = () => errWithCode("EBADF", "bad file descriptor");
		const ENOTEMPTY = () => errWithCode("ENOTEMPTY", "directory not empty");

		// Console output buffering for stdout/stderr so partial lines are held
		// until a newline is written (matches upstream behaviour).
		const outputBufs = { 1: "", 2: "" };
		const consoleFor = (fd) => (fd === 2 ? console.error : console.log);

		const flushLine = (fd) => {
			const nl = outputBufs[fd].lastIndexOf("\n");
			if (nl !== -1) {
				consoleFor(fd)(outputBufs[fd].substring(0, nl));
				outputBufs[fd] = outputBufs[fd].substring(nl + 1);
			}
		};

		// --- fd table ---------------------------------------------------------

		const fdForPath = (pathStr) => {
			const fd = nextFD++;
			openFiles.set(fd, { path: normalize(pathStr), pos: 0 });
			return fd;
		};

		// --- fs implementation ------------------------------------------------

		globalThis.fs = {
			// O_* constants. Go's fs_js.go reads these at init time. O_DIRECTORY is
			// intentionally OMITTED: when it is missing (undefined) Go leaves
			// nodeDIRECTORY = -1 and refuses O_DIRECTORY opens instead of failing
			// to start. Including it would make Go set nodeDIRECTORY and require
			// every open() to honour the flag.
			constants: {
				O_RDONLY: 0,
				O_WRONLY: 1,
				O_RDWR: 2,
				O_CREAT: 0x40,
				O_EXCL: 0x80,
				O_TRUNC: 0x200,
				O_APPEND: 0x400,
			},

			writeSync(fd, buf) {
				if (fd === 1 || fd === 2) {
					outputBufs[fd] += decoder.decode(buf);
					flushLine(fd);
					return buf.length;
				}
				// Synchronous write to a file descriptor. Best-effort: perform the
				// write and return the number of bytes written.
				const of = openFiles.get(fd);
				if (!of) {
					throw EBADF();
				}
				const data = files.get(of.path);
				if (!data) {
					throw ENOENT(of.path);
				}
				const merged = new Uint8Array(of.pos + buf.length);
				merged.set(data.subarray(0, of.pos), 0);
				merged.set(buf, of.pos);
				files.set(of.path, merged);
				of.pos += buf.length;
				mtimes.set(of.path, now());
				return buf.length;
			},

			write(fd, buf, offset, length, position, callback) {
				// fds 1/2 are the console (stdout/stderr). Go's syscall.Write builds
				// a fresh Uint8Array and calls write(fd, buf, 0, len, null), so route
				// those straight to the console.
				if (fd === 1 || fd === 2) {
					try {
						const n = this.writeSync(fd, buf.subarray(offset, offset + length));
						callback(null, n);
					} catch (err) {
						callback(err);
					}
					return;
				}

				const of = openFiles.get(fd);
				if (!of) {
					callback(EBADF());
					return;
				}
				const pathStr = of.path;

				// Position null means "use the fd's current position".
				let pos = position === null ? of.pos : position;

				if (isDir(pathStr)) {
					callback(EISDIR());
					return;
				}

				try {
					let data = files.get(pathStr);
					if (!data) {
						callback(ENOENT(pathStr));
						return;
					}
					const slice = buf.subarray(offset, offset + length);

					if (pos + length > data.length) {
						// Grow the file to accommodate the write.
						const grown = new Uint8Array(pos + length);
						grown.set(data, 0);
						data = grown;
					}
					data.set(slice, pos);

					files.set(pathStr, data);
					mtimes.set(pathStr, now());

					if (position === null) {
						of.pos = pos + length;
					}
					callback(null, length);
				} catch (err) {
					callback(err);
				}
			},

			read(fd, buffer, offset, length, position, callback) {
				// stdin (fd 0) is never readable here.
				if (fd === 0) {
					callback(null, 0, buffer);
					return;
				}

				const of = openFiles.get(fd);
				if (!of) {
					callback(EBADF());
					return;
				}
				const pathStr = of.path;

				if (isDir(pathStr)) {
					callback(EISDIR());
					return;
				}

				try {
					const data = files.get(pathStr);
					if (!data) {
						callback(ENOENT(pathStr));
						return;
					}
					// position null => current offset of the open file description.
					const pos = position === null ? of.pos : position;
					if (pos >= data.length) {
						// EOF
						callback(null, 0, buffer);
						return;
					}
					const avail = data.length - pos;
					const n = Math.min(length, avail);
					buffer.set(data.subarray(pos, pos + n), offset);
					if (position === null) {
						of.pos = pos + n;
					}
					callback(null, n, buffer);
				} catch (err) {
					callback(err);
				}
			},

			open(pathStr, flags, mode, callback) {
				try {
					const p = normalize(pathStr);
					const exists = files.has(p) || isDir(p);

					// O_CREAT | O_EXCL: fail if the file already exists.
					if ((flags & 0x40) !== 0 && (flags & 0x80) !== 0 && exists) {
						callback(EEXIST());
						return;
					}

					// Opening a non-existent path without O_CREAT is an error.
					if (!exists && (flags & 0x40) === 0) {
						callback(ENOENT(p));
						return;
					}

					if ((flags & 0x40) !== 0 && !exists) {
						// Create an empty regular file.
						files.set(p, new Uint8Array(0));
						mtimes.set(p, now());
					} else if ((flags & 0x200) !== 0) {
						// O_TRUNC: truncate an existing regular file to zero length.
						if (files.has(p)) {
							files.set(p, new Uint8Array(0));
							mtimes.set(p, now());
						}
					}

					// For O_APPEND, start the cursor at end-of-file.
					const fd = fdForPath(p);
					if ((flags & 0x400) !== 0) {
						const of = openFiles.get(fd);
						const data = files.get(p);
						of.pos = data ? data.length : 0;
					}

					callback(null, fd);
				} catch (err) {
					callback(err);
				}
			},

			close(fd, callback) {
				// fds 0/1/2 are the standard streams; nothing to close.
				if (fd === 0 || fd === 1 || fd === 2) {
					callback(null);
					return;
				}
				openFiles.delete(fd);
				callback(null);
			},

			fstat(fd, callback) {
				const of = openFiles.get(fd);
				if (!of) {
					callback(EBADF());
					return;
				}
				const p = of.path;
				const data = files.get(p);
				const size = data ? data.length : 0;
				callback(null, makeStats(p, isDir(p), size));
			},

			stat(pathStr, callback) {
				const p = normalize(pathStr);
				if (files.has(p)) {
					const data = files.get(p);
					callback(null, makeStats(p, false, data.length));
					return;
				}
				if (isDir(p)) {
					callback(null, makeStats(p, true, 0));
					return;
				}
				callback(ENOENT(p));
			},

			lstat(pathStr, callback) {
				// There are no symlinks in this filesystem, so lstat == stat.
				return this.stat(pathStr, callback);
			},

			readdir(pathStr, callback) {
				const p = normalize(pathStr);
				if (files.has(p)) {
					callback(ENOTDIR());
					return;
				}
				if (!isDir(p)) {
					callback(ENOENT(p));
					return;
				}
				const prefix = p === "/" ? "/" : p + "/";
				const entries = new Set();
				// Collect the immediate child names of both files and explicit dirs.
				for (const key of files.keys()) {
					if (key.startsWith(prefix)) {
						const rest = key.substring(prefix.length);
						const slash = rest.indexOf("/");
						if (slash === -1) {
							entries.add(rest);
						} else {
							entries.add(rest.substring(0, slash));
						}
					}
				}
				for (const d of dirs) {
					if (d.startsWith(prefix)) {
						const rest = d.substring(prefix.length);
						const slash = rest.indexOf("/");
						if (slash === -1 && rest !== "") {
							entries.add(rest);
						}
					}
				}
				callback(null, Array.from(entries));
			},

			mkdir(pathStr, perm, callback) {
				const p = normalize(pathStr);
				if (files.has(p) || dirs.has(p) || isImplicitDir(p)) {
					callback(EEXIST());
					return;
				}
				dirs.add(p);
				mtimes.set(p, now());
				callback(null);
			},

			unlink(pathStr, callback) {
				const p = normalize(pathStr);
				if (files.has(p)) {
					files.delete(p);
					mtimes.delete(p);
					callback(null);
					return;
				}
				// Cannot unlink a directory.
				callback(isDir(p) ? EISDIR() : ENOENT(p));
			},

			rmdir(pathStr, callback) {
				const p = normalize(pathStr);
				if (files.has(p)) {
					callback(ENOTDIR());
					return;
				}
				if (!isDir(p)) {
					callback(ENOENT(p));
					return;
				}
				if (hasChildren(p)) {
					// The directory still contains files/dirs beneath it.
					callback(ENOTEMPTY());
					return;
				}
				dirs.delete(p);
				mtimes.delete(p);
				callback(null);
			},

			rename(from, to, callback) {
				const src = normalize(from);
				const dst = normalize(to);
				try {
					if (files.has(src)) {
						if (isDir(dst)) {
							callback(EISDIR());
							return;
						}
						files.set(dst, files.get(src));
						files.delete(src);
						mtimes.set(dst, now());
						mtimes.delete(src);
						callback(null);
						return;
					}
					if (dirs.has(src)) {
						// Renaming an explicitly-created directory. Move the dir entry
						// and any files beneath it.
						if (files.has(dst)) {
							callback(ENOTDIR());
							return;
						}
						const srcPrefix = src + "/";
						const dstPrefix = dst + "/";
						for (const key of Array.from(files.keys())) {
							if (key.startsWith(srcPrefix)) {
								files.set(dstPrefix + key.substring(srcPrefix.length), files.get(key));
								files.delete(key);
							}
						}
						dirs.delete(src);
						dirs.add(dst);
						mtimes.set(dst, now());
						mtimes.delete(src);
						callback(null);
						return;
					}
					callback(ENOENT(src));
				} catch (err) {
					callback(err);
				}
			},

			// --- operations Go may call but that we stub out ----------------------
			// These all succeed as no-ops or return ENOSYS so the syscall layer is
			// happy. fsync in particular is expected to succeed.

			fsync(fd, callback) { callback(null); },
			chmod(path, mode, callback) { callback(null); },
			fchmod(fd, mode, callback) { callback(null); },
			chown(path, uid, gid, callback) { callback(null); },
			fchown(fd, uid, gid, callback) { callback(null); },
			lchown(path, uid, gid, callback) { callback(null); },
			utimes(path, atime, mtime, callback) { callback(null); },
			truncate(pathStr, length, callback) {
				const p = normalize(pathStr);
				if (!files.has(p)) { callback(ENOENT(p)); return; }
				const data = files.get(p);
				if (length < data.length) {
					files.set(p, data.subarray(0, length));
				} else if (length > data.length) {
					const grown = new Uint8Array(length);
					grown.set(data, 0);
					files.set(p, grown);
				}
				mtimes.set(p, now());
				callback(null);
			},
			ftruncate(fd, length, callback) { callback(null); },
			readlink(path, callback) { callback(ENOENT(path)); },
			symlink(target, path, callback) { callback(null); },
			link(existingPath, newPath, callback) { callback(null); },
		};
	}

	// ---------------------------------------------------------------------------
	// globalThis.process / globalThis.path shims
	// ---------------------------------------------------------------------------

	if (!globalThis.process) {
		globalThis.process = {
			getuid() { return -1; },
			getgid() { return -1; },
			geteuid() { return -1; },
			getegid() { return -1; },
			getgroups() { return []; },
			pid: -1,
			ppid: -1,
			umask() { return 0; },
			cwd() { return "/"; },
			chdir() { },
			platform: "browser",
		}
	}

	if (!globalThis.path) {
		const normalizePath = (p) => {
			const isAbs = p.length > 0 && p[0] === "/";
			const parts = p.split("/");
			const stack = [];
			for (const part of parts) {
				if (part === "" || part === ".") {
					continue;
				}
				if (part === "..") {
					if (stack.length > 0) {
						stack.pop();
					}
					continue;
				}
				stack.push(part);
			}
			let result = stack.join("/");
			if (isAbs || result === "") {
				result = "/" + result;
			}
			return result;
		};

		globalThis.path = {
			resolve(...pathSegments) {
				if (pathSegments.length === 0) {
					return "/";
				}
				// Resolve right-to-left, prepending segments until an absolute path
				// is found (Node.js path.resolve semantics). Always returns absolute.
				let combined = "";
				for (let i = pathSegments.length - 1; i >= 0; i--) {
					const seg = pathSegments[i];
					combined = seg + "/" + combined;
					if (seg.length > 0 && seg[0] === "/") {
						break;
					}
				}
				if (combined.length === 0 || combined[0] !== "/") {
					combined = "/" + combined;
				}
				return normalizePath(combined);
			},
			join(...paths) {
				return normalizePath(paths.join("/"));
			},
			normalize: normalizePath,
		}
	}

	if (!globalThis.crypto) {
		throw new Error("globalThis.crypto is not available, polyfill required (crypto.getRandomValues only)");
	}

	if (!globalThis.performance) {
		throw new Error("globalThis.performance is not available, polyfill required (performance.now only)");
	}

	if (!globalThis.TextEncoder) {
		throw new Error("globalThis.TextEncoder is not available, polyfill required");
	}

	if (!globalThis.TextDecoder) {
		throw new Error("globalThis.TextDecoder is not available, polyfill required");
	}

	const encoder = new TextEncoder("utf-8");
	const decoder = new TextDecoder("utf-8");

	globalThis.Go = class {
		constructor() {
			this.argv = ["js"];
			this.env = {};
			this.exit = (code) => {
				if (code !== 0) {
					console.warn("exit code:", code);
				}
			};
			this._exitPromise = new Promise((resolve) => {
				this._resolveExitPromise = resolve;
			});
			this._pendingEvent = null;
			this._scheduledTimeouts = new Map();
			this._nextCallbackTimeoutID = 1;

			const setInt64 = (addr, v) => {
				this.mem.setUint32(addr + 0, v, true);
				this.mem.setUint32(addr + 4, Math.floor(v / 4294967296), true);
			}

			const setInt32 = (addr, v) => {
				this.mem.setUint32(addr + 0, v, true);
			}

			const getInt64 = (addr) => {
				const low = this.mem.getUint32(addr + 0, true);
				const high = this.mem.getInt32(addr + 4, true);
				return low + high * 4294967296;
			}

			const loadValue = (addr) => {
				const f = this.mem.getFloat64(addr, true);
				if (f === 0) {
					return undefined;
				}
				if (!isNaN(f)) {
					return f;
				}

				const id = this.mem.getUint32(addr, true);
				return this._values[id];
			}

			const storeValue = (addr, v) => {
				const nanHead = 0x7FF80000;

				if (typeof v === "number" && v !== 0) {
					if (isNaN(v)) {
						this.mem.setUint32(addr + 4, nanHead, true);
						this.mem.setUint32(addr, 0, true);
						return;
					}
					this.mem.setFloat64(addr, v, true);
					return;
				}

				if (v === undefined) {
					this.mem.setFloat64(addr, 0, true);
					return;
				}

				let id = this._ids.get(v);
				if (id === undefined) {
					id = this._idPool.pop();
					if (id === undefined) {
						id = this._values.length;
					}
					this._values[id] = v;
					this._goRefCounts[id] = 0;
					this._ids.set(v, id);
				}
				this._goRefCounts[id]++;
				let typeFlag = 0;
				switch (typeof v) {
					case "object":
						if (v !== null) {
							typeFlag = 1;
						}
						break;
					case "string":
						typeFlag = 2;
						break;
					case "symbol":
						typeFlag = 3;
						break;
					case "function":
						typeFlag = 4;
						break;
				}
				this.mem.setUint32(addr + 4, nanHead | typeFlag, true);
				this.mem.setUint32(addr, id, true);
			}

			const loadSlice = (addr) => {
				const array = getInt64(addr + 0);
				const len = getInt64(addr + 8);
				return new Uint8Array(this._inst.exports.mem.buffer, array, len);
			}

			const loadSliceOfValues = (addr) => {
				const array = getInt64(addr + 0);
				const len = getInt64(addr + 8);
				const a = new Array(len);
				for (let i = 0; i < len; i++) {
					a[i] = loadValue(array + i * 8);
				}
				return a;
			}

			const loadString = (addr) => {
				const saddr = getInt64(addr + 0);
				const len = getInt64(addr + 8);
				return decoder.decode(new DataView(this._inst.exports.mem.buffer, saddr, len));
			}

			const testCallExport = (a, b) => {
				this._inst.exports.testExport0();
				return this._inst.exports.testExport(a, b);
			}

			const timeOrigin = Date.now() - performance.now();
			this.importObject = {
				_gotest: {
					add: (a, b) => a + b,
					callExport: testCallExport,
				},
				gojs: {
					// Go's SP does not change as long as no Go code is running. Some operations (e.g. calls, getters and setters)
					// may synchronously trigger a Go event handler. This makes Go code get executed in the middle of the imported
					// function. A goroutine can switch to a new stack if the current stack is too small (see morestack function).
					// This changes the SP, thus we have to update the SP used by the imported function.

					// func wasmExit(code int32)
					"runtime.wasmExit": (sp) => {
						sp >>>= 0;
						const code = this.mem.getInt32(sp + 8, true);
						this.exited = true;
						delete this._inst;
						delete this._values;
						delete this._goRefCounts;
						delete this._ids;
						delete this._idPool;
						this.exit(code);
					},

					// func wasmWrite(fd uintptr, p unsafe.Pointer, n int32)
					"runtime.wasmWrite": (sp) => {
						sp >>>= 0;
						const fd = getInt64(sp + 8);
						const p = getInt64(sp + 16);
						const n = this.mem.getInt32(sp + 24, true);
						fs.writeSync(fd, new Uint8Array(this._inst.exports.mem.buffer, p, n));
					},

					// func resetMemoryDataView()
					"runtime.resetMemoryDataView": (sp) => {
						sp >>>= 0;
						this.mem = new DataView(this._inst.exports.mem.buffer);
					},

					// func nanotime1() int64
					"runtime.nanotime1": (sp) => {
						sp >>>= 0;
						setInt64(sp + 8, (timeOrigin + performance.now()) * 1000000);
					},

					// func walltime() (sec int64, nsec int32)
					"runtime.walltime": (sp) => {
						sp >>>= 0;
						const msec = (new Date).getTime();
						setInt64(sp + 8, msec / 1000);
						this.mem.setInt32(sp + 16, (msec % 1000) * 1000000, true);
					},

					// func scheduleTimeoutEvent(delay int64) int32
					"runtime.scheduleTimeoutEvent": (sp) => {
						sp >>>= 0;
						const id = this._nextCallbackTimeoutID;
						this._nextCallbackTimeoutID++;
						this._scheduledTimeouts.set(id, setTimeout(
							() => {
								this._resume();
								while (this._scheduledTimeouts.has(id)) {
									// for some reason Go failed to register the timeout event, log and try again
									// (temporary workaround for https://github.com/golang/go/issues/28975)
									console.warn("scheduleTimeoutEvent: missed timeout event");
									this._resume();
								}
							},
							getInt64(sp + 8),
						));
						this.mem.setInt32(sp + 16, id, true);
					},

					// func clearTimeoutEvent(id int32)
					"runtime.clearTimeoutEvent": (sp) => {
						sp >>>= 0;
						const id = this.mem.getInt32(sp + 8, true);
						clearTimeout(this._scheduledTimeouts.get(id));
						this._scheduledTimeouts.delete(id);
					},

					// func getRandomData(r []byte)
					"runtime.getRandomData": (sp) => {
						sp >>>= 0;
						crypto.getRandomValues(loadSlice(sp + 8));
					},

					// func finalizeRef(v ref)
					"syscall/js.finalizeRef": (sp) => {
						sp >>>= 0;
						const id = this.mem.getUint32(sp + 8, true);
						this._goRefCounts[id]--;
						if (this._goRefCounts[id] === 0) {
							const v = this._values[id];
							this._values[id] = null;
							this._ids.delete(v);
							this._idPool.push(id);
						}
					},

					// func stringVal(value string) ref
					"syscall/js.stringVal": (sp) => {
						sp >>>= 0;
						storeValue(sp + 24, loadString(sp + 8));
					},

					// func valueGet(v ref, p string) ref
					"syscall/js.valueGet": (sp) => {
						sp >>>= 0;
						const result = Reflect.get(loadValue(sp + 8), loadString(sp + 16));
						sp = this._inst.exports.getsp() >>> 0; // see comment above
						storeValue(sp + 32, result);
					},

					// func valueSet(v ref, p string, x ref)
					"syscall/js.valueSet": (sp) => {
						sp >>>= 0;
						Reflect.set(loadValue(sp + 8), loadString(sp + 16), loadValue(sp + 32));
					},

					// func valueDelete(v ref, p string)
					"syscall/js.valueDelete": (sp) => {
						sp >>>= 0;
						Reflect.deleteProperty(loadValue(sp + 8), loadString(sp + 16));
					},

					// func valueIndex(v ref, i int) ref
					"syscall/js.valueIndex": (sp) => {
						sp >>>= 0;
						storeValue(sp + 24, Reflect.get(loadValue(sp + 8), getInt64(sp + 16)));
					},

					// valueSetIndex(v ref, i int, x ref)
					"syscall/js.valueSetIndex": (sp) => {
						sp >>>= 0;
						Reflect.set(loadValue(sp + 8), getInt64(sp + 16), loadValue(sp + 24));
					},

					// func valueCall(v ref, m string, args []ref) (ref, bool)
					"syscall/js.valueCall": (sp) => {
						sp >>>= 0;
						try {
							const v = loadValue(sp + 8);
							const m = Reflect.get(v, loadString(sp + 16));
							const args = loadSliceOfValues(sp + 32);
							const result = Reflect.apply(m, v, args);
							sp = this._inst.exports.getsp() >>> 0; // see comment above
							storeValue(sp + 56, result);
							this.mem.setUint8(sp + 64, 1);
						} catch (err) {
							sp = this._inst.exports.getsp() >>> 0; // see comment above
							storeValue(sp + 56, err);
							this.mem.setUint8(sp + 64, 0);
						}
					},

					// func valueInvoke(v ref, args []ref) (ref, bool)
					"syscall/js.valueInvoke": (sp) => {
						sp >>>= 0;
						try {
							const v = loadValue(sp + 8);
							const args = loadSliceOfValues(sp + 16);
							const result = Reflect.apply(v, undefined, args);
							sp = this._inst.exports.getsp() >>> 0; // see comment above
							storeValue(sp + 40, result);
							this.mem.setUint8(sp + 48, 1);
						} catch (err) {
							sp = this._inst.exports.getsp() >>> 0; // see comment above
							storeValue(sp + 40, err);
							this.mem.setUint8(sp + 48, 0);
						}
					},

					// func valueNew(v ref, args []ref) (ref, bool)
					"syscall/js.valueNew": (sp) => {
						sp >>>= 0;
						try {
							const v = loadValue(sp + 8);
							const args = loadSliceOfValues(sp + 16);
							const result = Reflect.construct(v, args);
							sp = this._inst.exports.getsp() >>> 0; // see comment above
							storeValue(sp + 40, result);
							this.mem.setUint8(sp + 48, 1);
						} catch (err) {
							sp = this._inst.exports.getsp() >>> 0; // see comment above
							storeValue(sp + 40, err);
							this.mem.setUint8(sp + 48, 0);
						}
					},

					// func valueLength(v ref) int
					"syscall/js.valueLength": (sp) => {
						sp >>>= 0;
						setInt64(sp + 16, parseInt(loadValue(sp + 8).length));
					},

					// valuePrepareString(v ref) (ref, int)
					"syscall/js.valuePrepareString": (sp) => {
						sp >>>= 0;
						const str = encoder.encode(String(loadValue(sp + 8)));
						storeValue(sp + 16, str);
						setInt64(sp + 24, str.length);
					},

					// valueLoadString(v ref, b []byte)
					"syscall/js.valueLoadString": (sp) => {
						sp >>>= 0;
						const str = loadValue(sp + 8);
						loadSlice(sp + 16).set(str);
					},

					// func valueInstanceOf(v ref, t ref) bool
					"syscall/js.valueInstanceOf": (sp) => {
						sp >>>= 0;
						this.mem.setUint8(sp + 24, (loadValue(sp + 8) instanceof loadValue(sp + 16)) ? 1 : 0);
					},

					// func copyBytesToGo(dst []byte, src ref) (int, bool)
					"syscall/js.copyBytesToGo": (sp) => {
						sp >>>= 0;
						const dst = loadSlice(sp + 8);
						const src = loadValue(sp + 32);
						if (!(src instanceof Uint8Array || src instanceof Uint8ClampedArray)) {
							this.mem.setUint8(sp + 48, 0);
							return;
						}
						const toCopy = src.subarray(0, dst.length);
						dst.set(toCopy);
						setInt64(sp + 40, toCopy.length);
						this.mem.setUint8(sp + 48, 1);
					},

					// func copyBytesToJS(dst ref, src []byte) (int, bool)
					"syscall/js.copyBytesToJS": (sp) => {
						sp >>>= 0;
						const dst = loadValue(sp + 8);
						const src = loadSlice(sp + 16);
						if (!(dst instanceof Uint8Array || dst instanceof Uint8ClampedArray)) {
							this.mem.setUint8(sp + 48, 0);
							return;
						}
						const toCopy = src.subarray(0, dst.length);
						dst.set(toCopy);
						setInt64(sp + 40, toCopy.length);
						this.mem.setUint8(sp + 48, 1);
					},

					"debug": (value) => {
						console.log(value);
					},
				}
			};
		}

		async run(instance) {
			if (!(instance instanceof WebAssembly.Instance)) {
				throw new Error("Go.run: WebAssembly.Instance expected");
			}
			this._inst = instance;
			this.mem = new DataView(this._inst.exports.mem.buffer);
			this._values = [ // JS values that Go currently has references to, indexed by reference id
				NaN,
				0,
				null,
				true,
				false,
				globalThis,
				this,
			];
			this._goRefCounts = new Array(this._values.length).fill(Infinity); // number of references that Go has to a JS value, indexed by reference id
			this._ids = new Map([ // mapping from JS values to reference ids
				[0, 1],
				[null, 2],
				[true, 3],
				[false, 4],
				[globalThis, 5],
				[this, 6],
			]);
			this._idPool = [];   // unused ids that have been garbage collected
			this.exited = false; // whether the Go program has exited

			// Pass command line arguments and environment variables to WebAssembly by writing them to the linear memory.
			let offset = 4096;

			const strPtr = (str) => {
				const ptr = offset;
				const bytes = encoder.encode(str + "\0");
				new Uint8Array(this.mem.buffer, offset, bytes.length).set(bytes);
				offset += bytes.length;
				if (offset % 8 !== 0) {
					offset += 8 - (offset % 8);
				}
				return ptr;
			};

			const argc = this.argv.length;

			const argvPtrs = [];
			this.argv.forEach((arg) => {
				argvPtrs.push(strPtr(arg));
			});
			argvPtrs.push(0);

			const keys = Object.keys(this.env).sort();
			keys.forEach((key) => {
				argvPtrs.push(strPtr(`${key}=${this.env[key]}`));
			});
			argvPtrs.push(0);

			const argv = offset;
			argvPtrs.forEach((ptr) => {
				this.mem.setUint32(offset, ptr, true);
				this.mem.setUint32(offset + 4, 0, true);
				offset += 8;
			});

			// The linker guarantees global data starts from at least wasmMinDataAddr.
			// Keep in sync with cmd/link/internal/ld/data.go:wasmMinDataAddr.
			const wasmMinDataAddr = 4096 + 8192;
			if (offset >= wasmMinDataAddr) {
				throw new Error("total length of command line and environment variables exceeds limit");
			}

			this._inst.exports.run(argc, argv);
			if (this.exited) {
				this._resolveExitPromise();
			}
			await this._exitPromise;
		}

		_resume() {
			if (this.exited) {
				throw new Error("Go program has already exited");
			}
			this._inst.exports.resume();
			if (this.exited) {
				this._resolveExitPromise();
			}
		}

		_makeFuncWrapper(id) {
			const go = this;
			return function () {
				const event = { id: id, this: this, args: arguments };
				go._pendingEvent = event;
				go._resume();
				return event.result;
			};
		}
	}
})();
