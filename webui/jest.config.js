module.exports = {
  testEnvironment: 'jsdom',
  transform: {
    '^.+\\.(tsx?|jsx?)$': [
      'babel-jest',
      {
        presets: [['babel-preset-react-app', { runtime: 'automatic' }]],
      },
    ],
  },
  transformIgnorePatterns: [
    'node_modules/(?!(?:@codemirror|@marijn|@lezer|crelt|style-mod|w3c-keyname)/)',
  ],
  moduleNameMapper: {
    '\\.(css|less|scss|sass)$': 'identity-obj-proxy',
    '\\.(jpg|jpeg|png|gif|svg|ico|webp)$': '<rootDir>/src/__mocks__/fileMock.js',
    // Map @sprout/ui and @sprout/events to source TypeScript for tests.
    // This avoids the dual-React problem from the bundled CJS/ESM output.
    '^@sprout/ui$': '<rootDir>/../packages/ui/src/index.ts',
    '^@sprout/ui/(.*)$': '<rootDir>/../packages/ui/src/$1',
    '^@sprout/events$': '<rootDir>/../packages/events/src/index.ts',
  },
};
