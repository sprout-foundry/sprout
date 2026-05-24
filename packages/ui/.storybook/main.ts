import type { StorybookConfig } from '@storybook/react-vite';

const config: StorybookConfig = {
  stories: ['../src/**/*.stories.@(js|jsx|mjs|ts|tsx)'],
  addons: [
    '@storybook/addon-links',
    '@storybook/addon-essentials',
    '@chromatic-com/storybook',
  ],
  framework: {
    name: '@storybook/react-vite',
    options: {},
  },
  typescript: {
    reactDocgen: 'react-docgen-typescript',
    reactDocgenTypescriptOptions: {
      shouldExtractLiteralValuesFromEnum: true,
      propFilter: (prop) => (prop.parent ? !/node_modules/.test(prop.parent.fileName) : true),
    },
    check: false,
    tsconfigPath: './tsconfig.storybook.json',
  },
  docs: {
    autodocs: 'tag',
  },
  async viteFinal(config) {
    // Remove vite-plugin-dts from Storybook build — it uses tsconfig.build.json
    // which has rootDir: "src" causing TS6059 errors with fixture imports
    const filteredPlugins = config.plugins?.filter(
      (p: any) => !['vite:dts', 'vite-plugin-dts'].includes(p?.name)
    ) ?? [];
    return {
      ...config,
      plugins: filteredPlugins,
      esbuild: {
        tsconfigRaw: JSON.stringify({
          compilerOptions: {
            rootDir: '.',
          },
        }),
      },
    };
  },
};

export default config;
