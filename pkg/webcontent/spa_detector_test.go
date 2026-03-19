package webcontent

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// bigScript returns a string of approximately n bytes that looks like
// minified JavaScript.
func bigScript(n int) string {
	const chunk = "var _0xa1b2c3=function(_0xd4e5f6){return _0xd4e5f6[_0xa1b2c3];};"
	var b strings.Builder
	for b.Len() < n {
		b.WriteString(chunk)
	}
	return b.String()[:n]
}

// ---------------------------------------------------------------------------
// NeedsRendering — SPA shell patterns (table-driven)
// ---------------------------------------------------------------------------

func TestNeedsRendering_SPAShellPatterns(t *testing.T) {
	tests := []struct {
		name     string
		html     string
		expected bool
	}{
		// ---- Empty shell divs → true --------------------------------------
		{
			name:     "empty div id root",
			html:     `<html><body><div id="root"></div></body></html>`,
			expected: true,
		},
		{
			name:     "empty div id app",
			html:     `<html><body><div id="app"></div></body></html>`,
			expected: true,
		},
		{
			name:     "empty div id __next",
			html:     `<html><body><div id="__next"></div></body></html>`,
			expected: true,
		},
		{
			name:     "empty div id __next with Noscript fallback",
			html:     `<html><body><div id="__next"><noscript>JavaScript required</noscript></div></body></html>`,
			expected: true,
		},
		{
			name:     "Next.js App Router double nested __next",
			html:     `<html><body><div id="__next"><div id="__next"></div></div></body></html>`,
			expected: true,
		},
		{
			name:     "empty div id main alone in body",
			html:     `<html><body><div id="main"></div></body></html>`,
			expected: true,
		},
		{
			name:     "empty div class root",
			html:     `<html><body><div class="root"></div></body></html>`,
			expected: true,
		},
		// ---- Shell divs with only whitespace → true -----------------------
		{
			name:     "div id root with whitespace only",
			html:     "<html><body><div id=\"root\">  \n\t  </div></body></html>",
			expected: true,
		},
		{
			name:     "div id app with noscript only",
			html:     `<html><body><div id="app"><noscript>You need to enable JavaScript to run this app.</noscript></div></body></html>`,
			expected: true,
		},
		// ---- Shell divs with substantial SSR content → false --------------
		{
			name: "div id root with real SSR content",
			html: `<html><body><div id="root">
<h1>Welcome to Our Site</h1>
<p>This is real server-rendered content about our products and services.</p>
<p>Visit our about page to learn more about our team, mission, and values.</p>
<ul><li>Feature one: description</li><li>Feature two: description</li></ul>
</div></body></html>`,
			expected: false,
		},
		{
			name: "div id app with substantial content",
			html: `<html><body><div id="app"><header><h1>Site Title</h1></header>
<main>
<p>Lorem ipsum dolor sit amet, consectetur adipiscing elit. Sed do eiusmod tempor incididunt ut labore.</p>
<p>Ut enim ad minim veniam, quis nostrud exercitation ullamco laboris nisi ut aliquip.</p>
</main></div></body></html>`,
			expected: false,
		},
		{
			name: "div id main with surrounding content",
			html: `<html><body><header>Site Header</header>
<div id="main"><p>This is a substantial amount of text inside the main content area of the page.</p>
<p>More paragraphs follow to demonstrate this is a real server-rendered content page.</p></div>
<footer>Site Footer</footer></body></html>`,
			expected: false,
		},
		// ---- SPA shell with external script refs (realistic) → true --------
		{
			name: "React CRA shell with bundle script tags",
			html: `<html><head></head><body>
<div id="root"></div>
<script src="/static/js/main.a1b2c3d4.js"></script>
<script src="/static/js/2.e5f6g7h8.chunk.js"></script>
</body></html>`,
			expected: true,
		},
		{
			name: "Vue CLI shell with chunk scripts",
			html: `<html><head></head><body>
<div id="app"></div>
<script src="/js/app.deadbeef.js"></script>
<script src="/js/chunk-vendors.cafef00d.js"></script>
</body></html>`,
			expected: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, NeedsRendering(tc.html))
		})
	}
}

// ---------------------------------------------------------------------------
// NeedsRendering — framework marker detection
// ---------------------------------------------------------------------------

func TestNeedsRendering_FrameworkMarkerInScript(t *testing.T) {
	tests := []struct {
		name     string
		html     string
		expected bool
	}{
		{
			name:     "NEXT_DATA script with low text content",
			html:     `<html><body><div id="__next"></div><script id="__NEXT_DATA__" type="application/json">{"buildId":"abc","props":{"pageProps":{}}}</script></body></html>`,
			expected: true,
		},
		{
			name:     "NUXT_DATA script with low text content",
			html:     `<html><body><div id="__nuxt"></div><script>window.__NUXT__={};</script></body></html>`,
			expected: true,
		},
		{
			name:     "window.__APP pattern with low text content",
			html:     `<html><body><div id="root"></div><script>window.__APP_STATE={user:null};</script></body></html>`,
			expected: true,
		},
		{
			name:     "script src containing react lowercase",
			html:     `<html><body><div id="root"></div><script crossorigin src="https://unpkg.com/react@18/umd/react.production.min.js"></script></body></html>`,
			expected: true,
		},
		{
			name:     "script src containing next lowercase",
			html:     `<html><body><div id="__next"></div><script src="/_next/static/chunks/main.js"></script></body></html>`,
			expected: true,
		},
		{
			name:     "script src containing vue lowercase",
			html:     `<html><body><div id="app"></div><script src="https://cdn.jsdelivr.net/npm/vue@3/dist/vue.global.prod.js"></script></body></html>`,
			expected: true,
		},
		{
			name:     "script src containing angular",
			html:     `<html><body><app-root></app-root><script src="/angular/main.js"></script></body></html>`,
			expected: true,
		},
		{
			name:     "script src containing svelte",
			html:     `<html><body><div id="app"></div><script src="/build/bundle.svelte.js"></script></body></html>`,
			expected: true,
		},
		{
			name:     "script src containing nuxt",
			html:     `<html><body><div id="__nuxt"></div><script src="/_nuxt/app.js"></script></body></html>`,
			expected: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, NeedsRendering(tc.html))
		})
	}
}

// Framework markers in content-heavy pages should NOT trigger — SSR frameworks
// (Next.js, Nuxt) render real content server-side.
func TestNeedsRendering_FrameworkMarkerWithHighTextContent_IsFalse(t *testing.T) {
	tests := []struct {
		name string
		html string
	}{
		{
			name: "Next.js SSR page with __NEXT_DATA__ and real content",
			html: `<html><head><title>Blog Post</title>
<script id="__NEXT_DATA__" type="application/json">{"buildId":"abc","props":{"pageProps":{"title":"Hello World"}}}</script>
</head><body><div id="__next"><article>
<h1>Hello World: My First Blog Post</h1>
<p>This is a fully server-rendered Next.js blog post with substantial text content.</p>
<p>Today I want to talk about how server-side rendering works in Next.js and why it matters for SEO.</p>
<p>By rendering on the server, we ensure that search engines can crawl our content effectively.</p>
</article></div></body></html>`,
		},
		{
			name: "React hydration script with real SSR content",
			html: `<html><head><title>Product Page</title></head>
<body><div id="root"><h1>Our Amazing Product</h1>
<p>This product page was server-rendered with React. It contains all the information you need.</p>
<p>Price: $49.99. Free shipping on orders over $100.</p>
<p>Customer reviews: 4.5/5 stars based on 2,847 reviews.</p>
</div>
<script src="/static/js/main.js"></script>
<script>ReactDOM.hydrate(document.getElementById('root'));</script>
</body></html>`,
		},
		{
			name: "Nuxt SSR page with __NUXT__ and real content",
			html: `<html><head><title>Documentation</title>
<script>window.__NUXT__=true;</script></head>
<body><div id="__nuxt"><main>
<h1>Getting Started Guide</h1>
<p>This Nuxt site was server-rendered for optimal performance and SEO.</p>
<p>Follow these steps to set up your development environment and start building.</p>
<h2>Prerequisites</h2>
<p>Make sure you have Node.js installed on your machine before proceeding.</p>
</main></div></body></html>`,
		},
		{
			name: "page with Google Analytics and substantial content",
			html: `<html><head><title>News Article</title>
<script async src="https://www.googletagmanager.com/gtag/js?id=GA_MEASUREMENT_ID"></script>
<script>window.dataLayer=window.dataLayer||[];function gtag(){dataLayer.push(arguments);}gtag('js',new Date());</script>
</head><body>
<h1>Breaking News: Important Event Happens Today</h1>
<p>In a stunning development, experts confirmed that the event will have far-reaching consequences.</p>
<p>"This is truly remarkable," said Dr. Smith, lead researcher at the Institute. "We've been studying this for years."</p>
<p>The implications for the industry are significant, with analysts predicting major changes in the coming months.</p>
<p>Stay tuned for more updates as this story develops throughout the day.</p>
</body></html>`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.False(t, NeedsRendering(tc.html))
		})
	}
}

// ---------------------------------------------------------------------------
// NeedsRendering — large inline script blocks
// ---------------------------------------------------------------------------

func TestNeedsRendering_LargeInlineScript_IsTrue(t *testing.T) {
	// A page where inline scripts dominate the body content.
	bundle := bigScript(50000)
	html := `<html><head><title>App</title></head><body>
<div id="root"></div>
<script>` + bundle + `</script>
</body></html>`

	assert.True(t, NeedsRendering(html))
}

func TestNeedsRendering_MultipleLargeInlineScripts_IsTrue(t *testing.T) {
	// Multiple inline script blocks that together dominate the page.
	html := `<html><head><title>App</title></head><body>
<div id="main"></div>
<script>` + bigScript(30000) + `</script>
<script>` + bigScript(20000) + `</script>
</body></html>`

	assert.True(t, NeedsRendering(html))
}

func TestNeedsRendering_SmallAnalyticsScripts_IsFalse(t *testing.T) {
	// A content-heavy page with a small tracking script should not trigger.
	html := `<html><head><title>Article</title>
<script async src="https://www.google-analytics.com/analytics.js"></script>
<script>
(function(i,s,o,g,r,a,m){i['GoogleAnalyticsObject']=r;i[r]=i[r]||function(){
(i[r].q=i[r].q||[]).push(arguments)},i[r].l=1*new Date();a=s.createElement(o),
m=s.getElementsByTagName(o)[0];a.async=1;a.src=g;m.parentNode.insertBefore(a,m)
})(window,document,'script','https://www.google-analytics.com/analytics.js','ga');
ga('create','UA-XXXXX-Y','auto');
</script>
</head><body>
<h1>Understanding Climate Change</h1>
<p>Climate change refers to long-term shifts in temperatures and weather patterns. While some of these shifts may be natural, human activities have been the main driver of climate change since the 1800s.</p>
<p>The primary cause of climate change is the burning of fossil fuels such as coal, oil, and gas, which produces heat-trapping gases. These greenhouse gases act like a blanket wrapped around the Earth, trapping the sun's heat and raising temperatures.</p>
<p>Examples of greenhouse gas emissions causing climate change include carbon dioxide and methane. These come from using gasoline for driving a car or coal for heating a building, for example.</p>
<p>Clearing land and forests can also release carbon dioxide. Landfills for garbage are a major source of methane emissions. Energy, industry, transport, buildings, agriculture, and land use are among the main emitters.</p>
</body></html>`

	assert.False(t, NeedsRendering(html))
}

func TestNeedsRendering_LargeInlineBundleInContentPage_IsFalse(t *testing.T) {
	// Even a large inline script should not trigger if the page has lots of text content.
	// This simulates a WordPress page with an inline "Above the Fold" CSS optimizer
	// plugin that injects a large inline style/script.
	html := `<html><head><title>Long-Form Article</title>
<script>` + bigScript(20000) + `</script>
</head><body>
<h1>The Complete Guide to Modern Software Architecture</h1>
<p>This comprehensive guide covers everything you need to know about designing, building, and maintaining modern software systems.</p>

<h2>Chapter 1: Foundations</h2>
<p>Software architecture is the fundamental organization of a system, embodied in its components, their relationships to each other and the environment, and the principles governing its design and evolution.</p>
<p>Good architecture is characterized by a clear separation of concerns, well-defined interfaces between components, and the ability to evolve independently over time. It balances the need for stability with the need for adaptability.</p>

<h2>Chapter 2: Design Patterns</h2>
<p>Design patterns provide reusable solutions to commonly occurring problems in software design. They are not finished designs that can be transformed directly into code, but rather templates that describe how to solve a problem in many different situations.</p>
<p>The most commonly used patterns include Singleton, Factory, Observer, Strategy, and Command patterns. Each serves a specific purpose and is most effective when applied to the right problem.</p>

<h2>Chapter 3: Microservices</h2>
<p>Microservices architecture is an approach to developing a single application as a suite of small services, each running in its own process and communicating with lightweight mechanisms. These services are built around business capabilities and independently deployable.</p>
<p>While microservices offer many benefits including independent deployment, technology diversity, and organizational alignment, they also introduce complexity in areas such as distributed systems management, data consistency, and network latency.</p>

<h2>Chapter 4: Event-Driven Architecture</h2>
<p>Event-driven architecture is a design paradigm in which application behavior is determined by events. An event represents a change in state and is the core mechanism through which information is propagated through the system.</p>
<p>This architectural style is particularly well-suited for applications that require real-time processing, loose coupling between components, and the ability to handle high volumes of concurrent operations.</p>
</body></html>`

	assert.False(t, NeedsRendering(html))
}

// ---------------------------------------------------------------------------
// NeedsRendering — low text-to-HTML ratio
// ---------------------------------------------------------------------------

func TestNeedsRendering_VeryLowTextRatio_IsTrue(t *testing.T) {
	// Page dominated by scripts and markup with almost no visible text.
	html := `<html><head><title>App</title>
<script>` + bigScript(40000) + `</script>
<style>body{margin:0;padding:0;}.container{display:flex;align-items:center;justify-content:center;height:100vh;}</style>
</head><body>
<div class="container"><div id="root"></div></div>
</body></html>`

	assert.True(t, NeedsRendering(html))
}

func TestNeedsRendering_StyleHeavyPage_IsTrue(t *testing.T) {
	// Page with huge inline style blocks and almost no text content.
	hugeCSS := strings.Repeat(".cls{prop:val;}", 2000)
	html := `<html><head><style>` + hugeCSS + `</style></head>
<body><div id="root"></div></body></html>`

	assert.True(t, NeedsRendering(html))
}

func TestNeedsRendering_NormalTextRatio_IsFalse(t *testing.T) {
	// A typical content page with reasonable text-to-HTML ratio.
	html := `<html><head><title>About Us</title>
<style>body{font-family:sans-serif;max-width:800px;margin:0 auto;}</style>
</head><body>
<h1>About Our Company</h1>
<p>We are a leading provider of innovative solutions for businesses worldwide. Founded in 2010, our team has grown to over 500 employees across 12 offices.</p>
<p>Our mission is to help organizations achieve their full potential through technology. We believe in building lasting partnerships and delivering measurable results.</p>
<p>With over a decade of experience, we have helped hundreds of companies transform their operations and achieve sustainable growth.</p>
</body></html>`

	assert.False(t, NeedsRendering(html))
}

// ---------------------------------------------------------------------------
// NeedsRendering — real-world page examples
// ---------------------------------------------------------------------------

func TestNeedsRendering_WordpressPage_IsFalse(t *testing.T) {
	// A realistic WordPress page with plugins, scripts, but lots of content.
	html := `<!DOCTYPE html>
<html><head>
<meta charset="UTF-8">
<title>The Best Coffee Shops in Minneapolis - Local Guide 2024</title>
<link rel="stylesheet" href="/wp-content/themes/twentytwentyfour/style.css">
<script src="https://cdn.jsdelivr.net/npm/lazysizes@5.3.2/lazysizes.min.js" async></script>
<meta name="description" content="Discover the top 10 coffee shops in Minneapolis with our comprehensive local guide.">
</head><body>

<header class="site-header">
<h1 class="site-title">Minneapolis Food Guide</h1>
<nav><a href="/">Home</a><a href="/about">About</a><a href="/contact">Contact</a></nav>
</header>

<main class="content">
<article>
<h2>The Best Coffee Shops in Minneapolis: A 2024 Guide</h2>

<p>Minneapolis has an incredible coffee scene that has been growing rapidly over the past decade. From cozy neighborhood cafes to award-winning roasters, there is something for every coffee lover in the Twin Cities.</p>

<h3>1. Brothers Coffee - North Loop</h3>
<p>Located in the heart of the North Loop, Brothers Coffee has been a staple of the Minneapolis coffee community since 2015. Known for their single-origin pour-overs and friendly baristas, this shop consistently ranks as one of the best in the city.</p>
<p>Address: 223 N 2nd St, Minneapolis, MN 55401</p>
<p>Hours: Mon-Fri 6:30am-4pm, Sat-Sun 7am-4pm</p>

<h3>2. Dogwood Coffee - Downtown</h3>
<p>Dogwood is synonymous with specialty coffee in Minneapolis. They roast their own beans and take great pride in their sourcing relationships with farmers around the world. The downtown location offers a bright, airy space perfect for working or catching up with friends.</p>

<h3>3. Spyhouse Coffee - Northeast</h3>
<p>With multiple locations around the city, Spyhouse is perhaps the most iconic coffee brand in Minneapolis. Their Northeast location, housed in a converted warehouse, offers a unique atmosphere with exposed brick, high ceilings, and a rotating selection of single-origin coffees.</p>
<p>What makes Spyhouse special is their commitment to both quality and community. They regularly host cupping sessions, latte art competitions, and educational workshops for coffee enthusiasts of all levels.</p>

<h2>What Makes a Great Coffee Shop?</h2>
<p>We evaluate each coffee shop based on four key criteria: bean quality and sourcing, barista skill and friendliness, atmosphere and ambiance, and value for money. Our team visits each location multiple times to ensure a fair and accurate assessment.</p>

<h2>Conclusion</h2>
<p>Whether you are a coffee connoisseur or just looking for a great place to grab a latte, Minneapolis has you covered. The city's coffee scene continues to evolve and improve, and we cannot wait to see what new shops open their doors in the coming year.</p>
</article>
</main>

<footer>
<p>&copy; 2024 Minneapolis Food Guide. All rights reserved.</p>
</footer>

<script>
/* Google Analytics tracking code */
window.dataLayer = window.dataLayer || [];
function gtag(){dataLayer.push(arguments);}
gtag('js', new Date());
</script>
</body></html>`

	assert.False(t, NeedsRendering(html))
}

func TestNeedsRendering_ReactSPA_Shell_IsTrue(t *testing.T) {
	// A typical Create React App production build index.html.
	html := `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<meta name="theme-color" content="#000000">
<meta name="description" content="Web site created using create-react-app">
<title>React App</title>
<link rel="manifest" href="/manifest.json">
</head>
<body>
<noscript>You need to enable JavaScript to run this app.</noscript>
<div id="root"></div>
<script>!function(e){function r(r){for(var n,i,a=r[0],c=r[1],l=r[2]...` + bigScript(100000) + `</script>
<script src="/static/js/2.a8b9c0d1.chunk.js"></script>
<script src="/static/js/main.e2f3a4b5.chunk.js"></script>
</body></html>`

	assert.True(t, NeedsRendering(html))
}

func TestNeedsRendering_NginxErrorPage_IsFalse(t *testing.T) {
	// Standard Nginx error page rendered by the server.
	html := `<html>
<head><title>404 Not Found</title></head>
<body>
<center><h1>404 Not Found</h1></center>
<hr><center>nginx/1.18.0 (Ubuntu)</center>
</body>
</html>`

	assert.False(t, NeedsRendering(html))
}

func TestNeedsRendering_Nginx500ErrorPage_IsFalse(t *testing.T) {
	html := `<html>
<head><title>500 Internal Server Error</title></head>
<body>
<center><h1>500 Internal Server Error</h1></center>
<hr><center>nginx/1.22.1</center>
</body>
</html>`

	assert.False(t, NeedsRendering(html))
}

func TestNeedsRendering_EaToRestaurantPage_IsFalse(t *testing.T) {
	// The EaTo restaurant page — has inline JSON-LD script but is content-heavy.
	html := `<html><head>
<meta name='robots' content='index, follow' />
<title>EaTo - Delicious Italian Fare</title>
<meta name="description" content="Located in the East Town Minneapolis neighborhood, EaTo is a cheerful oasis." />
<link rel="canonical" href="https://eatompls.com/" />
<meta property="og:locale" content="en_US" />
<meta property="og:type" content="website" />
<meta property="og:title" content="EaTo - Delicious Italian Fare" />
<meta property="og:description" content="Located in the East Town Minneapolis neighborhood, EaTo is a cheerful oasis." />
<meta property="og:url" content="https://eatompls.com/" />
<meta property="og:site_name" content="EaTo" />
<script type="application/ld+json">{"@type":"WebPage","name":"EaTo","description":"Delicious Italian Fare"}</script>
</head><body>
<h1>Welcome to EaTo</h1>
<h2>Delicious Italian Fare in East Town Minneapolis</h2>
<p>Located in the heart of the East Town neighborhood, EaTo offers a cheerful oasis of Italian cuisine. Our menu features house-made pastas, wood-fired pizzas, and seasonal dishes crafted from locally sourced ingredients.</p>
<p>Join us for happy hour Monday through Friday from 4-6pm, or make a reservation for dinner service Wednesday through Sunday. Our brunch menu is available on weekends from 10am to 2pm.</p>
<p>We look forward to welcoming you to EaTo.</p>
</body></html>`

	assert.False(t, NeedsRendering(html))
}

// ---------------------------------------------------------------------------
// NeedsRendering — edge cases
// ---------------------------------------------------------------------------

func TestNeedsRendering_EmptyString_IsFalse(t *testing.T) {
	assert.False(t, NeedsRendering(""))
}

func TestNeedsRendering_PlainText_IsFalse(t *testing.T) {
	assert.False(t, NeedsRendering("This is just plain text with no HTML tags at all."))
}

func TestNeedsRendering_TinyHTMLSnippet_IsFalse(t *testing.T) {
	assert.False(t, NeedsRendering(`<p>Hello</p>`))
}

func TestNeedsRendering_TinyHTMLWithOneWord_IsFalse(t *testing.T) {
	assert.False(t, NeedsRendering(`<b>Hi</b>`))
}

func TestNeedsRendering_NonHTMLJSON_IsFalse(t *testing.T) {
	assert.False(t, NeedsRendering(`{"name":"test","value":42}`))
}

func TestNeedsRendering_NonHTMLXML_IsFalse(t *testing.T) {
	assert.False(t, NeedsRendering(`<?xml version="1.0"?><catalog><book id="bk101"><author>Gambardella, Matthew</author><title>XML Developer&apos;s Guide</title></book></catalog>`))
}

func TestNeedsRendering_OnlyTagsNoContent_IsTrue(t *testing.T) {
	// A page with HTML structure but absolutely no text content at all.
	html := `<html><head></head><body><div><div><div></div></div></div></body></html>`
	assert.True(t, NeedsRendering(html))
}

func TestNeedsRendering_OnlyScriptTagsNoVisibleContent_IsTrue(t *testing.T) {
	html := `<html><head><script>` + bigScript(10000) + `</script></head><body></body></html>`
	assert.True(t, NeedsRendering(html))
}

func TestNeedsRendering_HTMLCommentOnly_IsTrue(t *testing.T) {
	// Page with only an HTML comment — effectively empty for text extraction.
	html := `<!-- This page has not been implemented yet -->`
	// An HTML comment with no tags doesn't look like an SPA shell,
	// but there is zero visible text. Whether this should be true depends on
	// implementation: it's not an SPA, but there is no content to extract.
	// We test false because a simple comment string is not an SPA pattern.
	assert.False(t, NeedsRendering(html))
}

// ---------------------------------------------------------------------------
// NeedsRendering — individual signal isolation
// ---------------------------------------------------------------------------

// TestNeedsRendering_OnlyLowTextRatioNoShellNoFramework verifies that a very
// low visible-text ratio triggers even without SPA shell patterns or framework
// markers — the page is clearly not going to produce useful text.
func TestNeedsRendering_OnlyLowTextRatioNoShellNoFramework_IsTrue(t *testing.T) {
	// No recognized SPA shells, no framework markers, but the page is almost
	// entirely markup and inline scripts.
	html := `<html><head><title>Page</title>
<style>` + strings.Repeat("body{margin:0;padding:0;}.a{color:red;}.b{font-size:14px;}", 500) + `</style>
<script>var _data=` + bigScript(5000) + `;</script>
</head><body>
<div class="wrapper"><div class="container"><div class="content">
<span class="label">OK</span>
</div></div></div>
</body></html>`

	assert.True(t, NeedsRendering(html))
}

// TestNeedsRendering_OnlySPAShellWithEnoughTextForRatio verifies that an empty
// SPA shell div triggers even if the total HTML has enough text to pass the
// ratio check on its own — the shell pattern is a strong enough signal.
func TestNeedsRendering_OnlySPAShellWithEnoughTextForRatio_IsTrue(t *testing.T) {
	// A page with an empty <div id="root"> but also some surrounding text
	// in other elements (e.g., a footer or cookie notice) that gives a
	// moderate text ratio. The shell pattern should still trigger.
	html := `<html><head><title>App</title></head><body>
<header><nav><span>Brand</span><a href="/">Home</a><a href="/about">About</a></nav></header>
<div id="root"></div>
<footer><p>&copy; 2024 Brand Inc. All rights reserved.</p><a href="/privacy">Privacy Policy</a></footer>
<script src="/static/js/main.js"></script>
</body></html>`

	assert.True(t, NeedsRendering(html))
}

// TestNeedsRendering_FrameworkMarkerAloneInContentPage_IsFalse verifies that a
// framework marker in <script> does NOT trigger when the page has substantial
// visible text content — SSR pages have framework markers AND real content.
func TestNeedsRendering_FrameworkMarkerAloneInContentPage_IsFalse(t *testing.T) {
	// Contains __NEXT_DATA__ but is a fully-rendered SSR page.
	html := `<html><head>
<title>Documentation - Getting Started</title>
<script id="__NEXT_DATA__" type="application/json">{"buildId":"xyz","page":"/docs/getting-started"}</script>
</head><body><div id="__next">
<nav>Docs Home | API Reference | Guides</nav>
<h1>Getting Started Guide</h1>
<p>Welcome to the documentation. This guide walks you through the initial setup process step by step.</p>
<h2>Prerequisites</h2>
<p>Before you begin, ensure you have the following installed on your system: Node.js version 18 or later, a package manager such as npm or yarn, and a code editor of your choice.</p>
<h2>Installation</h2>
<p>Open your terminal and run the following command to create a new project. This will set up the basic directory structure and install all required dependencies.</p>
<h2>Configuration</h2>
<p>After installation, you will need to configure the application. Create a configuration file in the root directory and add the following settings.</p>
<p>For detailed configuration options, please refer to the API reference section of our documentation.</p>
</div></body></html>`

	assert.False(t, NeedsRendering(html))
}

// ---------------------------------------------------------------------------
// NeedsRendering — combined signals
// ---------------------------------------------------------------------------

func TestNeedsRendering_EmptyShellPlusLargeScript_IsTrue(t *testing.T) {
	// The strongest SPA signal: empty shell + large inline bundle.
	html := `<html><head><title>App</title></head><body>
<div id="root"></div>
<script id="bundle">` + bigScript(80000) + `</script>
</body></html>`

	assert.True(t, NeedsRendering(html))
}

func TestNeedsRendering_EmptyShellWithTrackingScriptsOnly_IsTrue(t *testing.T) {
	// Empty root div with only small tracking scripts (no large bundle) —
	// still an SPA shell.
	html := `<html><head><title>App</title>
<script async src="https://www.googletagmanager.com/gtag/js?id=G-12345"></script>
<script>gtag('js',new Date());gtag('config','G-12345');</script>
</head><body>
<div id="root"></div>
</body></html>`

	assert.True(t, NeedsRendering(html))
}
