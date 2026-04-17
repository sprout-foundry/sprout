// @ts-nocheck

import { createRoot, type Root } from 'react-dom/client';
import { act } from 'react';
import SettingsPanel from './SettingsPanel';

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

const mockOnSettingsChanged = jest.fn();
const mockOnRequestProviderSetup = jest.fn();

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const MINIMAL_SETTINGS = {
  config: {
    version: '2.0',
    last_used_provider: 'openrouter',
    provider_models: {
      openrouter: 'openai/gpt-5',
      openai: 'gpt-4',
      'ollama-local': 'qwen3-coder:30b',
      'ollama-turbo': 'deepseek-v3.1:671b',
      zai: 'GLM-4.6',
      deepinfra: 'deepseek-ai/DeepSeek-V3.1-Terminus',
    },
    provider_priority: ['openrouter', 'zai', 'deepinfra', 'ollama-turbo', 'ollama-local', 'openai'],
    commit_provider: '',
    commit_model: '',
    review_provider: '',
    review_model: '',
    subagent_provider: '',
    subagent_model: '',
    self_review_gate_mode: 'off',
  },
};

const SETTINGS_WITH_COMMIT_REVIEW = {
  ...MINIMAL_SETTINGS,
  config: {
    ...MINIMAL_SETTINGS.config,
    commit_provider: 'openai',
    commit_model: 'gpt-4',
    review_provider: 'ollama-local',
    review_model: 'qwen3-coder:30b',
  },
};

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

let container: HTMLDivElement | null = null;
let root: Root | null = null;

beforeEach(() => {
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
});

afterEach(() => {
  act(() => {
    root?.unmount();
    root = null;
  });
  container?.remove();
  container = null;
  mockOnSettingsChanged.mockClear();
  mockOnRequestProviderSetup.mockClear();
});

function renderSettingsPanel(settings: typeof MINIMAL_SETTINGS) {
  act(() => {
    root?.render(
      <SettingsPanel
        settings={settings}
        onSettingsChanged={mockOnSettingsChanged}
        onRequestProviderSetup={mockOnRequestProviderSetup}
      />
    );
  });
}

function getCommitReviewTab(): HTMLElement | null {
  return container?.querySelector('[data-testid="tab-commit-review"]') || null;
}

function getActiveTabContent(): HTMLElement | null {
  return container?.querySelector('.settings-tab-content.active') || null;
}

function getCommitProviderInput(): HTMLInputElement | null {
  return (container?.querySelector('[data-testid="commit-provider"]') as HTMLInputElement) || null;
}

function getCommitModelInput(): HTMLInputElement | null {
  return (container?.querySelector('[data-testid="commit-model"]') as HTMLInputElement) || null;
}

function getReviewProviderInput(): HTMLInputElement | null {
  return (container?.querySelector('[data-testid="review-provider"]') as HTMLInputElement) || null;
}

function getReviewModelInput(): HTMLInputElement | null {
  return (container?.querySelector('[data-testid="review-model"]') as HTMLInputElement) || null;
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('SettingsPanel - Commit & Review Tab', () => {
  test('renders with commit & review tab available', () => {
    renderSettingsPanel(MINIMAL_SETTINGS);

    const commitReviewTab = getCommitReviewTab();
    expect(commitReviewTab).toBeInTheDocument();
    expect(commitReviewTab?.textContent).toContain('Commit & Review');
  });

  test('commit & review tab can be activated', () => {
    renderSettingsPanel(MINIMAL_SETTINGS);

    const commitReviewTab = getCommitReviewTab();
    act(() => {
      commitReviewTab?.click();
    });

    const activeTabContent = getActiveTabContent();
    expect(activeTabContent).toBeInTheDocument();
  });

  test('displays commit provider and model settings', () => {
    renderSettingsPanel(SETTINGS_WITH_COMMIT_REVIEW);

    const commitReviewTab = getCommitReviewTab();
    act(() => {
      commitReviewTab?.click();
    });

    const commitProviderInput = getCommitProviderInput();
    const commitModelInput = getCommitModelInput();

    expect(commitProviderInput).toBeInTheDocument();
    expect(commitModelInput).toBeInTheDocument();
    expect(commitProviderInput?.value).toBe('openai');
    expect(commitModelInput?.value).toBe('gpt-4');
  });

  test('displays review provider and model settings', () => {
    renderSettingsPanel(SETTINGS_WITH_COMMIT_REVIEW);

    const commitReviewTab = getCommitReviewTab();
    act(() => {
      commitReviewTab?.click();
    });

    const reviewProviderInput = getReviewProviderInput();
    const reviewModelInput = getReviewModelInput();

    expect(reviewProviderInput).toBeInTheDocument();
    expect(reviewModelInput).toBeInTheDocument();
    expect(reviewProviderInput?.value).toBe('ollama-local');
    expect(reviewModelInput?.value).toBe('qwen3-coder:30b');
  });

  test('allows changing commit provider', () => {
    renderSettingsPanel(SETTINGS_WITH_COMMIT_REVIEW);

    const commitReviewTab = getCommitReviewTab();
    act(() => {
      commitReviewTab?.click();
    });

    const commitProviderInput = getCommitProviderInput();
    expect(commitProviderInput).toBeInTheDocument();

    const newValue = 'zai';
    act(() => {
      commitProviderInput!.value = newValue;
      commitProviderInput?.dispatchEvent(new Event('change', { bubbles: true }));
    });

    expect(commitProviderInput?.value).toBe(newValue);
    expect(mockOnSettingsChanged).toHaveBeenCalled();
  });

  test('allows changing commit model', () => {
    renderSettingsPanel(SETTINGS_WITH_COMMIT_REVIEW);

    const commitReviewTab = getCommitReviewTab();
    act(() => {
      commitReviewTab?.click();
    });

    const commitModelInput = getCommitModelInput();
    expect(commitModelInput).toBeInTheDocument();

    const newValue = 'GLM-4.6';
    act(() => {
      commitModelInput!.value = newValue;
      commitModelInput?.dispatchEvent(new Event('change', { bubbles: true }));
    });

    expect(commitModelInput?.value).toBe(newValue);
    expect(mockOnSettingsChanged).toHaveBeenCalled();
  });

  test('allows changing review provider', () => {
    renderSettingsPanel(SETTINGS_WITH_COMMIT_REVIEW);

    const commitReviewTab = getCommitReviewTab();
    act(() => {
      commitReviewTab?.click();
    });

    const reviewProviderInput = getReviewProviderInput();
    expect(reviewProviderInput).toBeInTheDocument();

    const newValue = 'openrouter';
    act(() => {
      reviewProviderInput!.value = newValue;
      reviewProviderInput?.dispatchEvent(new Event('change', { bubbles: true }));
    });

    expect(reviewProviderInput?.value).toBe(newValue);
    expect(mockOnSettingsChanged).toHaveBeenCalled();
  });

  test('allows changing review model', () => {
    renderSettingsPanel(SETTINGS_WITH_COMMIT_REVIEW);

    const commitReviewTab = getCommitReviewTab();
    act(() => {
      commitReviewTab?.click();
    });

    const reviewModelInput = getReviewModelInput();
    expect(reviewModelInput).toBeInTheDocument();

    const newValue = 'openai/gpt-5';
    act(() => {
      reviewModelInput!.value = newValue;
      reviewModelInput?.dispatchEvent(new Event('change', { bubbles: true }));
    });

    expect(reviewModelInput?.value).toBe(newValue);
    expect(mockOnSettingsChanged).toHaveBeenCalled();
  });

  test('shows empty values when not configured', () => {
    renderSettingsPanel(MINIMAL_SETTINGS);

    const commitReviewTab = getCommitReviewTab();
    act(() => {
      commitReviewTab?.click();
    });

    const commitProviderInput = getCommitProviderInput();
    const commitModelInput = getCommitModelInput();
    const reviewProviderInput = getReviewProviderInput();
    const reviewModelInput = getReviewModelInput();

    expect(commitProviderInput?.value).toBe('');
    expect(commitModelInput?.value).toBe('');
    expect(reviewProviderInput?.value).toBe('');
    expect(reviewModelInput?.value).toBe('');
  });

  test('commit and review configs are independent', () => {
    const settingsWithMixedConfig = {
      ...MINIMAL_SETTINGS,
      config: {
        ...MINIMAL_SETTINGS.config,
        commit_provider: 'openai',
        commit_model: 'gpt-4',
        review_provider: '', // Empty - should not affect commit
        review_model: '',
      },
    };

    renderSettingsPanel(settingsWithMixedConfig);

    const commitReviewTab = getCommitReviewTab();
    act(() => {
      commitReviewTab?.click();
    });

    const commitProviderInput = getCommitProviderInput();
    const reviewProviderInput = getReviewProviderInput();

    expect(commitProviderInput?.value).toBe('openai');
    expect(reviewProviderInput?.value).toBe('');
  });
});
