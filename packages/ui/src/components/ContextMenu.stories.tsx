import type { Meta, StoryObj } from '@storybook/react';
import { useState } from 'react';
import ContextMenu from './ContextMenu';

const meta = {
  title: 'Components/ContextMenu',
  component: ContextMenu,
  parameters: {
    layout: 'centered',
  },
  tags: ['autodocs'],
} satisfies Meta<typeof ContextMenu>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Open: Story = {
  render: () => (
    <div style={{ height: '400px', position: 'relative' }}>
      <p style={{ marginBottom: '100px' }}>Context menu is rendered at (200, 100)</p>
      <ContextMenu
        isOpen={true}
        x={200}
        y={100}
        onClose={() => {}}
      >
        <button className="context-menu-item">Copy</button>
        <button className="context-menu-item">Cut</button>
        <button className="context-menu-item">Paste</button>
        <div className="context-menu-divider" />
        <button className="context-menu-item">Select All</button>
        <div className="context-menu-divider" />
        <button className="context-menu-item danger">Delete</button>
      </ContextMenu>
    </div>
  ),
};

export const Closed: Story = {
  args: {
    isOpen: false,
    x: 200,
    y: 100,
    onClose: () => {},
  },
};

export const WithSubheaders: Story = {
  render: () => (
    <div style={{ height: '400px', position: 'relative' }}>
      <p style={{ marginBottom: '100px' }}>Context menu with grouped items</p>
      <ContextMenu
        isOpen={true}
        x={200}
        y={100}
        onClose={() => {}}
      >
        <div style={{ padding: '8px 12px', fontSize: '11px', textTransform: 'uppercase', color: '#888' }}>
          File Operations
        </div>
        <button className="context-menu-item">New File</button>
        <button className="context-menu-item">New Folder</button>
        <div className="context-menu-divider" />
        <div style={{ padding: '8px 12px', fontSize: '11px', textTransform: 'uppercase', color: '#888' }}>
          Edit
        </div>
        <button className="context-menu-item">Copy</button>
        <button className="context-menu-item">Paste</button>
        <div className="context-menu-divider" />
        <button className="context-menu-item danger">Delete</button>
      </ContextMenu>
    </div>
  ),
};

export const LongMenu: Story = {
  render: () => (
    <div style={{ height: '400px', position: 'relative' }}>
      <p style={{ marginBottom: '100px' }}>Long context menu (tests viewport clamping)</p>
      <ContextMenu
        isOpen={true}
        x={200}
        y={100}
        onClose={() => {}}
      >
        <button className="context-menu-item">Copy</button>
        <button className="context-menu-item">Cut</button>
        <button className="context-menu-item">Paste</button>
        <div className="context-menu-divider" />
        <button className="context-menu-item">Undo</button>
        <button className="context-menu-item">Redo</button>
        <div className="context-menu-divider" />
        <button className="context-menu-item">Find</button>
        <button className="context-menu-item">Replace</button>
        <button className="context-menu-item">Go to Line</button>
        <div className="context-menu-divider" />
        <button className="context-menu-item">Toggle Word Wrap</button>
        <button className="context-menu-item">Toggle Minimap</button>
        <button className="context-menu-item">Format Document</button>
        <div className="context-menu-divider" />
        <button className="context-menu-item">Open in External Editor</button>
        <button className="context-menu-item">Reveal in File Browser</button>
        <div className="context-menu-divider" />
        <button className="context-menu-item danger">Delete</button>
      </ContextMenu>
    </div>
  ),
};

export const EdgePosition: Story = {
  render: () => (
    <div style={{ height: '400px', position: 'relative' }}>
      <p style={{ marginBottom: '100px' }}>Context menu near right edge (tests boundary clamping)</p>
      <ContextMenu
        isOpen={true}
        x={window.innerWidth - 50}
        y={100}
        onClose={() => {}}
      >
        <button className="context-menu-item">Copy</button>
        <button className="context-menu-item">Cut</button>
        <div className="context-menu-divider" />
        <button className="context-menu-item danger">Delete</button>
      </ContextMenu>
    </div>
  ),
};

// Interactive example with click to open
export const Interactive: Story = {
  render: () => {
    const [isOpen, setIsOpen] = useState(false);
    const [position, setPosition] = useState({ x: 0, y: 0 });

    const handleClick = (e: React.MouseEvent) => {
      setPosition({ x: e.clientX, y: e.clientY });
      setIsOpen(true);
    };

    return (
      <div style={{ height: '400px', padding: '20px' }}>
        <p>Click anywhere to open context menu:</p>
        <div
          onClick={handleClick}
          style={{
            height: '300px',
            border: '2px dashed #ccc',
            borderRadius: '8px',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            cursor: 'pointer',
            background: '#f5f5f5',
          }}
        >
          Click here
        </div>
        <ContextMenu
          isOpen={isOpen}
          x={position.x}
          y={position.y}
          onClose={() => setIsOpen(false)}
        >
          <button className="context-menu-item">Copy</button>
          <button className="context-menu-item">Cut</button>
          <div className="context-menu-divider" />
          <button className="context-menu-item danger">Delete</button>
        </ContextMenu>
      </div>
    );
  },
};
