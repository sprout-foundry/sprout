module.exports = {
  testEnvironment: 'jsdom',
  transform: {
    '^.+\\.(tsx?|jsx?)$': [
      'babel-jest',
      {
        presets: ['babel-preset-react-app'],
      },
    ],
  },
  transformIgnorePatterns: [
    'node_modules/(?!(?:@codemirror|@marijn|@lezer|crelt|style-mod|w3c-keyname)/)',
  ],
  moduleNameMapper: {
    '\\.(css|less|scss|sass)$': 'identity-obj-proxy',
    '\\.(jpg|jpeg|png|gif|svg|ico|webp)$': '<rootDir>/src/__mocks__/fileMock.js',
  },
};
