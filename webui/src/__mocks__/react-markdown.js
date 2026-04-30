// Mock react-markdown for Jest (ESM module can't be parsed by default Jest config)
const React = require('react');
function ReactMarkdown({ children }) {
  return React.createElement('div', null, children);
}
module.exports = ReactMarkdown;
module.exports.default = ReactMarkdown;
