import React from 'react';

// Re-export types from GitPanel and git-types
export type { GitFile, GitSidebarPanelProps } from './GitPanel';
export type { GitStatusData, FileSection } from '../types/git-types';

// Placeholder component - will be replaced with actual GitSidebarPanel component
const GitSidebarPanel: React.FC = () => <div>GitSidebarPanel placeholder</div>;
export default GitSidebarPanel;
