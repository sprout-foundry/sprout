package tools

import (
	"context"
	"strings"
	"testing"
)

func symbolNames(entries []SymbolEntry) map[string]int {
	names := make(map[string]int, len(entries))
	for _, e := range entries {
		if _, seen := names[e.Name]; !seen {
			names[e.Name] = e.Line // the first declaration of a name
		}
	}
	return names
}

// checkRegexSymbols asserts what the extractor lists for a source file:
// every wanted name at its line, and none of the unwanted ones.
func checkRegexSymbols(t *testing.T, ext, src string, want map[string]int, unwanted ...string) {
	t.Helper()
	got := symbolNames(extractSymbolsViaRegex([]byte(src), ext))
	for name, line := range want {
		if got[name] != line {
			t.Errorf("%s: %q at line %d, want line %d (got %v)", ext, name, got[name], line, got)
		}
	}
	for _, name := range unwanted {
		if _, ok := got[name]; ok {
			t.Errorf("%s: listed %q, which is not a declaration", ext, name)
		}
	}
}

func TestRegexSymbolsRust(t *testing.T) {
	src := `use std::fmt;

pub struct Queue {
    items: Vec<u8>,
}

pub(crate) enum State { Idle, Busy }

pub trait Claim {
    fn claim(&self) -> bool;
}

impl Claim for Queue {
    fn claim(&self) -> bool { true }
}

impl<T> Holder<T> {
    pub async fn take_next(&mut self) -> Option<T> { None }
}

pub const MAX_ITEMS: usize = 8;
macro_rules! log_it { () => {} }
mod tests;
`
	checkRegexSymbols(t, ".rs", src, map[string]int{
		"struct Queue":         3,
		"enum State":           7,
		"trait Claim":          9,
		"fn claim":             10,
		"impl Claim for Queue": 13,
		"impl Holder":          17,
		"fn take_next":         18,
		"const MAX_ITEMS":      21,
		"macro log_it":         22,
		"mod tests":            23,
	})
}

func TestRegexSymbolsJavaLike(t *testing.T) {
	java := `package app;

@Service
public final class TaskQueue implements Queue {
    private final List<Task> tasks = new ArrayList<>();

    public TaskQueue(int size) {
        super();
    }

    @Override
    public Optional<Task> claimNext(String worker) throws IOException {
        if (tasks.isEmpty()) {
            return Optional.empty();
        }
        for (Task t : tasks) { log(t); }
        return helper(worker);
    }

    static <T> List<T> copyOf(List<T> in) { return in; }

    interface Listener { void onClaim(Task t); }
}

public record Point(int x, int y) {}
enum Color { RED }
`
	checkRegexSymbols(t, ".java", java, map[string]int{
		"class TaskQueue":    4,
		"method TaskQueue":   7,
		"method claimNext":   12,
		"method copyOf":      20,
		"interface Listener": 22,
		"record Point":       25,
		"enum Color":         26,
	}, "method if", "method for", "method helper", "method empty", "method super")

	csharp := `namespace App;

public sealed partial class Worker : IWorker
{
    public async Task<bool> RunAsync(CancellationToken ct)
    {
        using (var scope = Create()) { }
        return await Next(ct);
    }

    internal static int Count() => 0;
}

public record struct Range(int Start, int End);
`
	checkRegexSymbols(t, ".cs", csharp, map[string]int{
		"class Worker":    3,
		"method RunAsync": 5,
		"method Count":    11,
		"record Range":    14,
	}, "method using", "method Next")

	dart := `class Counter extends ChangeNotifier {
  int _count = 0;

  void increment() {
    notifyListeners();
  }

  Future<void> load() async {}
}

mixin Logging on Object {}
extension Words on String {}
`
	checkRegexSymbols(t, ".dart", dart, map[string]int{
		"class Counter":    1,
		"method increment": 4,
		"method load":      8,
		"mixin Logging":    11,
		"extension Words":  12,
	}, "method notifyListeners")
}

func TestRegexSymbolsC(t *testing.T) {
	src := `#include <stdio.h>
#define MAX(a, b) ((a) > (b) ? (a) : (b))

typedef struct queue {
    int size;
} queue_t;

struct node;
enum color { RED, GREEN };

static int queue_claim(struct queue *q, int worker);

static int queue_claim(struct queue *q, int worker)
{
    if (q->size == 0) {
        return -1;
    }
    printf("%d\n", worker);
    return 0;
}

const char *
name_of(int id) {
    return "x";
}

namespace app {
class Server : public Base {
  public:
    void start();
};

void Server::start() {
    run();
}
}
`
	checkRegexSymbols(t, ".c", src, map[string]int{
		"struct queue":       4,
		"enum color":         9,
		"func queue_claim":   13,
		"namespace app":      27,
		"class Server":       28,
		"func Server::start": 33,
	}, "struct node", "func printf", "func if", "func MAX", "func run")
}

func TestRegexSymbolsPHPLuaScala(t *testing.T) {
	checkRegexSymbols(t, ".php", `<?php
final class TaskQueue {
    public function claimNext(string $worker): ?Task {
        return $this->pick($worker);
    }
}
interface Claimable {}
function helper() {}
`, map[string]int{
		"class TaskQueue":     2,
		"function claimNext":  3,
		"interface Claimable": 7,
		"function helper":     8,
	}, "function pick")

	checkRegexSymbols(t, ".lua", `local M = {}

function M.claim(worker)
  return pick(worker)
end

local function helper() end
M.release = function(id) end
`, map[string]int{
		"function M.claim":   3,
		"function helper":    7,
		"function M.release": 8,
	}, "function pick")

	checkRegexSymbols(t, ".scala", `package app

sealed trait Event
final case class Claimed(id: Long) extends Event
object Queue {
  def claimNext(worker: String): Option[Task] = None
  private[app] lazy val size = 0
}
`, map[string]int{
		"trait Event":   3,
		"class Claimed": 4,
		"object Queue":  5,
		"def claimNext": 6,
	})
}

// A query finds a regex-only language's file by what it declares, not only
// by its path.
func TestGenerateRepoMapQueryFindsRegexDeclarations(t *testing.T) {
	dir := t.TempDir()
	createTestFiles(t, dir, map[string]string{
		"src/lib.rs":   "pub fn claim_next() {}\n",
		"src/main.rs":  "fn main() { claim_next(); }\n",
		"src/other.rs": "fn unrelated() {}\n",
	})
	out, err := GenerateRepoMap(context.Background(), dir, 3, "claim next")
	requireErr(t, err, "repo map")
	if !strings.Contains(out, "src/lib.rs") || !strings.Contains(out, "fn claim_next:1") {
		t.Fatalf("the declaring file is missing:\n%s", out)
	}
	if strings.Contains(out, "src/other.rs") {
		t.Fatalf("listed a file that neither declares nor mentions the query:\n%s", out)
	}
}
