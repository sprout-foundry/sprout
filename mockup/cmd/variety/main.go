// Command variety draws a fixed set of ideas and reports how alike their
// first looks are: the kit, palette, composition and fonts each chose, the
// pairwise similarity of their first screens, and any invented ratings.
//
//	VARIETY_API_KEY=... go run ./cmd/variety -base https://api.deepinfra.com/v1/openai -model deepseek-ai/DeepSeek-V4.1-Flash -out /tmp/variety
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/sprout-foundry/sprout/mockup"
	"github.com/sprout-foundry/sprout/mockup/kit"
	"github.com/sprout-foundry/sprout/mockup/variety"
)

// ideas are builder ideas of the kinds people bring: local services,
// makers, events, small apps.
var ideas = []struct{ id, goal string }{
	{"cakes", "Rosa's Cakes: a website for a home bakery where people see the cakes and order one, choosing size, flavour and collection day."},
	{"walks", "Paws on Pine: a dog-walking business site with services and prices, the neighbourhoods covered, and a form to request a walk."},
	{"pottery", "Clay & Ember: a pottery studio's page with the class times, what to bring, and a way to book a seat."},
	{"party", "Maya's 30th: an invitation for a birthday dinner with the date, place, and an RSVP form saying who is coming and any food needs."},
	{"piano", "Keys with Kate: a piano teacher's booking page where a student picks a lesson time from the week and sees their next lesson."},
	{"cafe", "Lantern Café: a café menu with drinks, food, prices and opening hours, easy to read on a phone."},
	{"chores", "Chore Chart: a family app where each person sees their chores for the week and ticks them off, with points."},
	{"plumber", "Hartley Plumbing: an emergency plumber's site with services, the area covered, prices to call out, and a call-back request."},
	{"yoga", "Still Point Yoga: a yoga studio's timetable, teachers, first-class offer and a sign-up form."},
	{"books", "Neighbourhood Book Swap: a small app where neighbours list books to lend and ask to borrow one."},
}

func main() {
	base := flag.String("base", "https://api.deepinfra.com/v1/openai", "OpenAI-compatible API base URL")
	model := flag.String("model", "deepseek-ai/DeepSeek-V4.1-Flash", "model to draw with")
	out := flag.String("out", "variety-out", "where to write the drawn pages and the report")
	parallel := flag.Int("parallel", 3, "draws at a time")
	only := flag.String("only", "", "comma-separated idea ids to draw (default all)")
	thinking := flag.Bool("thinking", false, "let the model think first (the platform draws with thinking off)")
	person := flag.Bool("person", false, "draw the ideas one after another as one person's, each steering away from the looks before it")
	flag.Parse()
	key := os.Getenv("VARIETY_API_KEY")
	if key == "" {
		fmt.Fprintln(os.Stderr, "set VARIETY_API_KEY")
		os.Exit(2)
	}
	if err := os.MkdirAll(*out, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	stream := openAIStream(*base, *model, key, *thinking)

	pick := map[string]bool{}
	for _, id := range strings.Split(*only, ",") {
		if id = strings.TrimSpace(id); id != "" {
			pick[id] = true
		}
	}
	type drawn struct {
		id    string
		doc   string
		err   error
		print variety.Fingerprint
	}
	var (
		mu      sync.Mutex
		results []drawn
		wg      sync.WaitGroup
		slots   = make(chan struct{}, *parallel)
	)
	var recent []kit.Choice
	for _, idea := range ideas {
		if len(pick) > 0 && !pick[idea.id] {
			continue
		}
		wg.Add(1)
		draw := func(id, goal string) {
			defer wg.Done()
			slots <- struct{}{}
			defer func() { <-slots }()
			plan, _ := json.Marshal(map[string]any{"version": 1, "goal": goal, "steps": []map[string]string{{"description": "Build the first version of " + goal}}})
			mu.Lock()
			avoid := append([]kit.Choice(nil), recent...)
			mu.Unlock()
			started := time.Now()
			res, err := mockup.Draw(context.Background(), stream, mockup.Request{Plan: plan, Avoid: avoid}, nil)
			_ = os.WriteFile(filepath.Join(*out, id+".secs"), []byte(fmt.Sprintf("%.1f", time.Since(started).Seconds())), 0o644)
			if err == nil && *person {
				mu.Lock()
				recent = append([]kit.Choice{kit.ChoiceOf(res.HTML)}, recent...)
				mu.Unlock()
			}
			d := drawn{id: id, doc: res.HTML, err: err}
			if err == nil {
				d.print = variety.Of(res.HTML)
				_ = os.WriteFile(filepath.Join(*out, id+".html"), []byte(res.HTML), 0o644)
			}
			mu.Lock()
			results = append(results, d)
			mu.Unlock()
			fmt.Fprintf(os.Stderr, "drew %s (%v)\n", id, err)
		}
		if *person {
			draw(idea.id, idea.goal)
		} else {
			go draw(idea.id, idea.goal)
		}
	}
	wg.Wait()
	sort.Slice(results, func(i, j int) bool { return results[i].id < results[j].id })

	var r strings.Builder
	fmt.Fprintf(&r, "model %s, %s\n\n", *model, time.Now().UTC().Format(time.RFC3339))
	fmt.Fprintf(&r, "%-8s %-10s %-14s %-16s %-24s %s\n", "idea", "kit", "palette", "composition", "fonts", "invented proof")
	var ok []drawn
	for _, d := range results {
		if d.err != nil {
			fmt.Fprintf(&r, "%-8s failed: %v\n", d.id, d.err)
			continue
		}
		ok = append(ok, d)
		fmt.Fprintf(&r, "%-8s %-10s %-14s %-16s %-24s %s\n", d.id, d.print.Kit, d.print.Palette, dash(d.print.Composition),
			dash(d.print.Font), strings.Join(variety.InventedProof(d.doc), "; "))
	}
	kits, comps, parts := map[string]int{}, map[string]int{}, map[string]int{}
	var sum, max float64
	var maxPair string
	pairs := 0
	for i := range ok {
		kits[ok[i].print.Kit]++
		comps[dash(ok[i].print.Composition)]++
		for _, p := range ok[i].print.Parts {
			parts[p]++
		}
		for j := i + 1; j < len(ok); j++ {
			s := variety.Similarity(ok[i].print, ok[j].print)
			sum += s
			pairs++
			if s > max {
				max, maxPair = s, ok[i].id+"/"+ok[j].id
			}
		}
	}
	if pairs > 0 {
		fmt.Fprintf(&r, "\nfirst-screen similarity: mean %.2f, most alike %.2f (%s)\n", sum/float64(pairs), max, maxPair)
	}
	fmt.Fprintf(&r, "kits: %v\ncompositions: %v\n", kits, comps)
	var common []string
	for p, n := range parts {
		if n*2 > len(ok) {
			common = append(common, fmt.Sprintf("%s×%d", p, n))
		}
	}
	sort.Strings(common)
	fmt.Fprintf(&r, "parts in most first screens: %s\n", strings.Join(common, " "))
	_ = os.WriteFile(filepath.Join(*out, "report.txt"), []byte(r.String()), 0o644)
	fmt.Print(r.String())
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// openAIStream streams a chat completion from an OpenAI-compatible API.
func openAIStream(base, model, key string, thinking bool) mockup.StreamText {
	client := &http.Client{Timeout: 10 * time.Minute}
	return func(ctx context.Context, system, user string, onChunk func(string)) (string, error) {
		payload := map[string]any{
			"model":  model,
			"stream": true,
			"messages": []map[string]string{
				{"role": "system", "content": system},
				{"role": "user", "content": user},
			},
		}
		setThinking(payload, model, thinking)
		body, _ := json.Marshal(payload)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSuffix(base, "/")+"/chat/completions", bytes.NewReader(body))
		if err != nil {
			return "", err
		}
		req.Header.Set("Authorization", "Bearer "+key)
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			return "", err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			var b bytes.Buffer
			_, _ = b.ReadFrom(resp.Body)
			return "", fmt.Errorf("%s: %s", resp.Status, strings.TrimSpace(b.String()))
		}
		var all strings.Builder
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		for sc.Scan() {
			line := strings.TrimPrefix(sc.Text(), "data: ")
			if line == "" || line == "[DONE]" || strings.HasPrefix(line, ":") {
				continue
			}
			var ev struct {
				Choices []struct {
					Delta struct {
						Content string `json:"content"`
					} `json:"delta"`
				} `json:"choices"`
			}
			if json.Unmarshal([]byte(line), &ev) != nil || len(ev.Choices) == 0 {
				continue
			}
			if c := ev.Choices[0].Delta.Content; c != "" {
				all.WriteString(c)
				if onChunk != nil {
					onChunk(c)
				}
			}
		}
		return all.String(), sc.Err()
	}
}

// setThinking sets the model family's thinking switch the way the platform
// does; a family with no known switch keeps its provider's default.
func setThinking(payload map[string]any, model string, on bool) {
	m := strings.ToLower(model)
	switch {
	case strings.Contains(m, "qwen") || strings.Contains(m, "glm"):
		payload["chat_template_kwargs"] = map[string]any{"enable_thinking": on}
	case strings.Contains(m, "inkling"):
		effort := "none"
		if on {
			effort = "medium"
		}
		payload["reasoning_effort"] = effort
	case strings.Contains(m, "deepseek-v3") || strings.Contains(m, "deepseek-v4"):
		state := "disabled"
		if on {
			state = "enabled"
		}
		payload["thinking"] = map[string]any{"type": state}
	}
}
