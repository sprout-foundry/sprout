// @ts-check
import { defineConfig } from 'astro/config';

// Static output: `astro build` emits a fully static site into dist/.
// Astro's default build output is already static, so this config only
// pins the site URL used for canonical URLs and absolute links.
export default defineConfig({
  output: 'static',
  site: 'https://example.com',
});
