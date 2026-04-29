import type { Meta, StoryObj } from '@storybook/react';
import { useState, useCallback } from 'react';
import Editor, { type CursorPosition } from './Editor';

const meta = {
  title: 'Components/Editor',
  component: Editor,
  parameters: {
    layout: 'fullscreen',
  },
  tags: ['autodocs'],
} satisfies Meta<typeof Editor>;

export default meta;
type Story = StoryObj<typeof meta>;

const sampleCode = `import React, { useState, useEffect } from 'react';

interface Props {
  title: string;
  count?: number;
}

function App({ title, count = 0 }: Props) {
  const [value, setValue] = useState('');
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    // Initialize data on mount
    fetchData();
  }, []);

  const fetchData = async () => {
    setLoading(true);
    try {
      const response = await fetch('/api/data');
      const data = await response.json();
      setValue(data.value);
    } catch (error) {
      console.error('Failed to fetch data:', error);
    } finally {
      setLoading(false);
    }
  };

  const handleClick = () => {
    setValue(value + ' clicked');
  };

  return (
    <div className="container">
      <h1>{title}</h1>
      <p>Count: {count}</p>
      <button onClick={handleClick}>
        {loading ? 'Loading...' : 'Click me'}
      </button>
      <input
        type="text"
        value={value}
        onChange={(e) => setValue(e.target.value)}
      />
    </div>
  );
}

export default App;
`;

export const Default: Story = {
  args: {
    value: sampleCode,
    filePath: 'src/App.tsx',
    language: 'typescript',
    readOnly: false,
    wordWrap: false,
    fontSize: 13,
    fontFamily: "'JetBrains Mono', 'Fira Code', Menlo, Monaco, monospace",
    tabSize: 4,
    autoFocus: false,
  },
};

export const Empty: Story = {
  args: {
    value: '',
    filePath: 'src/new-file.ts',
    language: 'typescript',
    autoFocus: true,
  },
};

export const ReadOnly: Story = {
  args: {
    value: sampleCode,
    filePath: 'src/App.tsx',
    language: 'typescript',
    readOnly: true,
  },
};

export const WordWrap: Story = {
  args: {
    value: sampleCode,
    filePath: 'src/App.tsx',
    language: 'typescript',
    wordWrap: true,
  },
};

export const LargeFontSize: Story = {
  args: {
    value: sampleCode,
    filePath: 'src/App.tsx',
    language: 'typescript',
    fontSize: 18,
  },
};

export const SmallFontSize: Story = {
  args: {
    value: sampleCode,
    filePath: 'src/App.tsx',
    language: 'typescript',
    fontSize: 11,
  },
};

export const JavaScript: Story = {
  args: {
    value: `function greet(name) {
  return \`Hello, \${name}!\`;
}

class Calculator {
  constructor() {
    this.result = 0;
  }

  add(a, b) {
    this.result = a + b;
    return this;
  }

  multiply(a, b) {
    this.result = a * b;
    return this;
  }

  getResult() {
    return this.result;
  }
}

const calc = new Calculator();
calc.add(5, 10).multiply(2);
console.log(calc.getResult()); // 30
`,
    filePath: 'src/utils.js',
    language: 'javascript',
  },
};

export const Python: Story = {
  args: {
    value: `import asyncio
from typing import List, Optional

class DataProcessor:
    """A class to process data asynchronously."""

    def __init__(self, name: str):
        self.name = name
        self.data: List[dict] = []

    async def fetch_data(self, url: str) -> Optional[dict]:
        """Fetch data from a URL."""
        try:
            import aiohttp
            async with aiohttp.ClientSession() as session:
                async with session.get(url) as response:
                    return await response.json()
        except Exception as e:
            print(f"Error fetching data: {e}")
            return None

    def process(self, data: dict) -> dict:
        """Process the data."""
        return {
            'processed': True,
            'value': data.get('value', 0) * 2,
            'timestamp': datetime.now().isoformat()
        }

async def main():
    processor = DataProcessor("main")
    data = await processor.fetch_data("https://api.example.com/data")
    if data:
        result = processor.process(data)
        print(result)

if __name__ == "__main__":
    asyncio.run(main())
`,
    filePath: 'src/processor.py',
    language: 'python',
  },
};

export const CSS: Story = {
  args: {
    value: `.container {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  min-height: 100vh;
  padding: 20px;
  background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
}

.button {
  padding: 12px 24px;
  font-size: 16px;
  font-weight: 600;
  color: white;
  background: #007acc;
  border: none;
  border-radius: 8px;
  cursor: pointer;
  transition: all 0.3s ease;
}

.button:hover {
  background: #005a9e;
  transform: translateY(-2px);
  box-shadow: 0 4px 12px rgba(0, 122, 204, 0.4);
}

.button:active {
  transform: translateY(0);
}

@media (max-width: 768px) {
  .container {
    padding: 10px;
  }

  .button {
    font-size: 14px;
    padding: 10px 20px;
  }
}
`,
    filePath: 'src/styles.css',
    language: 'css',
  },
};

export const JSON: Story = {
  args: {
    value: `{
  "name": "my-project",
  "version": "1.0.0",
  "description": "A sample project",
  "main": "src/index.js",
  "scripts": {
    "dev": "next dev",
    "build": "next build",
    "start": "next start",
    "lint": "eslint src/**/*.{js,jsx,ts,tsx}",
    "test": "jest",
    "test:watch": "jest --watch",
    "test:coverage": "jest --coverage"
  },
  "dependencies": {
    "react": "^18.2.0",
    "react-dom": "^18.2.0",
    "next": "^13.4.0"
  },
  "devDependencies": {
    "@types/react": "^18.2.0",
    "@types/node": "^20.0.0",
    "typescript": "^5.0.0",
    "eslint": "^8.40.0",
    "jest": "^29.5.0"
  },
  "author": "John Doe",
  "license": "MIT"
}`,
    filePath: 'package.json',
    language: 'json',
  },
};

export const WithHighlightLine: Story = {
  args: {
    value: sampleCode,
    filePath: 'src/App.tsx',
    language: 'typescript',
    highlightLine: 15,
  },
};

export const WithAutoFocus: Story = {
  args: {
    value: '',
    filePath: 'src/new-file.ts',
    language: 'typescript',
    autoFocus: true,
  },
};

export const TwoSpacesIndent: Story = {
  args: {
    value: sampleCode,
    filePath: 'src/App.tsx',
    language: 'typescript',
    tabSize: 2,
  },
};

export const CustomFont: Story = {
  args: {
    value: sampleCode,
    filePath: 'src/App.tsx',
    language: 'typescript',
    fontFamily: "'Fira Code', 'Consolas', 'Monaco', monospace",
  },
};

export const Interactive: Story = {
  render: () => {
    const [code, setCode] = useState(sampleCode);
    const [cursor, setCursor] = useState<CursorPosition | null>(null);
    const [logs, setLogs] = useState<string[]>([]);

    const addLog = useCallback((message: string) => {
      setLogs((prev) => [...prev, `[${new Date().toLocaleTimeString()}] ${message}`]);
    }, []);

    const handleChange = useCallback((value: string) => {
      setCode(value);
      addLog(`Content changed (${value.length} characters)`);
    }, [addLog]);

    const handleSave = useCallback((value: string) => {
      addLog(`Saved content (${value.length} characters)`);
    }, [addLog]);

    const handleCursorChange = useCallback((position: CursorPosition) => {
      setCursor(position);
    }, []);

    const handleFocus = useCallback(() => {
      addLog('Editor focused');
    }, [addLog]);

    const handleBlur = useCallback(() => {
      addLog('Editor blurred');
    }, [addLog]);

    return (
      <div style={{ display: 'flex', flexDirection: 'column', height: '100vh' }}>
        <div style={{ padding: '10px', background: '#1e1e1e', color: '#d4d4d4', display: 'flex', alignItems: 'center', gap: '20px' }}>
          <span>Cursor: {cursor ? `Line ${cursor.line}, Column ${cursor.column}` : 'None'}</span>
          <span>Length: {code.length} chars</span>
        </div>
        <div style={{ flex: 1 }}>
          <Editor
            value={code}
            onChange={handleChange}
            onSave={handleSave}
            onCursorChange={handleCursorChange}
            onFocus={handleFocus}
            onBlur={handleBlur}
            filePath="src/App.tsx"
            language="typescript"
            autoFocus={true}
          />
        </div>
        <div style={{ height: '150px', background: '#1e1e1e', color: '#d4d4d4', padding: '10px', overflow: 'auto', fontFamily: 'monospace', fontSize: '12px' }}>
          <strong>Event Log:</strong>
          {logs.map((log, index) => (
            <div key={index}>{log}</div>
          ))}
        </div>
      </div>
    );
  },
};

export const LongFile: Story = {
  args: {
    value: `// This is a very long file to test scrolling performance

import React from 'react';
import { render } from 'react-dom';
import App from './App';

// Constants
const MAX_ITEMS = 1000;
const DEFAULT_TIMEOUT = 5000;

// Types
interface Item {
  id: number;
  name: string;
  description: string;
  created: Date;
}

// Utility functions
function formatDate(date: Date): string {
  return date.toISOString();
}

function generateId(): number {
  return Math.floor(Math.random() * MAX_ITEMS);
}

// Main function
function main() {
  console.log('Starting application...');
  const root = document.getElementById('root');
  if (!root) {
    throw new Error('Root element not found');
  }
  render(<App />, root);
  console.log('Application started');
}

${Array.from({ length: 100 }, (_, i) => `
// Comment block ${i}
export function function${i}(): void {
  const items: Item[] = [];
  for (let j = 0; j < 10; j++) {
    items.push({
      id: generateId(),
      name: \`Item \${j}\`,
      description: \`Description for item \${j}\`,
      created: new Date()
    });
  }
  return items;
}

export class Class${i} {
  private value: number;

  constructor(value: number) {
    this.value = value;
  }

  getValue(): number {
    return this.value;
  }

  setValue(value: number): void {
    this.value = value;
  }
}

const constant${i} = {
  name: 'Constant ${i}',
  value: i * 10,
  active: true
};
`).join('\n')}

// End of file
export default main;
`,
    filePath: 'src/very-long-file.ts',
    language: 'typescript',
  },
};
