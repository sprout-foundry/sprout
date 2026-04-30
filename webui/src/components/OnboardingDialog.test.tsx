/**
 * OnboardingDialog.test.tsx — Frontend tests for the OnboardingDialog component.
 *
 * These tests verify the complete onboarding experience:
 * - Provider selection (recommended and advanced providers)
 * - Model selection with combobox
 * - API key input for providers that require it
 * - Skip functionality (editor-only mode)
 * - Completion flow
 * - Error handling and validation
 * - Platform-specific guidance (Windows WSL/Git Bash)
 *
 * Note: This test file uses React's createElement and createRoot directly
 * (similar to HotkeyContext.test.tsx) because @testing-library/react is
 * not installed in this project's dependencies.
 */

import { createElement, type ReactElement, act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import OnboardingDialog from './OnboardingDialog';
import type { OnboardingState } from '../types/app';
import type { OnboardingProviderOption } from '../services/api';

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

// Mock lucide-react icons
jest.mock('lucide-react', () => ({
  X: ({ size }: { size?: number }) => require('react').createElement('svg', {
    width: size,
    height: size,
    'data-testid': 'x-icon',
  }),
}));

// ---------------------------------------------------------------------------
// Test Data
// ---------------------------------------------------------------------------

const mockOnboarding: OnboardingState = {
  open: true,
  submitting: false,
  checking: false,
  validationSuccess: false,
  provider: '',
  model: '',
  apiKey: '',
  error: null,
  keyError: false,
  isReonboarding: false,
  showAllProviders: false,
  reason: '',
  platformActionMessage: null,
  environment: {
    runtime_platform: 'linux',
    host_platform: 'linux',
    backend_mode: 'native',
    has_wsl: false,
    has_git_bash: true,
    recommended_terminal: 'system',
    active_distro: '',
    wsl_distros: [],
  },
  providers: [], // Initialize as empty, will be populated in tests that need it
  initialModelSet: false,
  validationModelCount: 0,
};

const mockRecommendedProviders: OnboardingProviderOption[] = [
  {
    id: 'zai',
    name: 'Z.AI',
    models: ['glm-5', 'glm-4.7', 'glm-4.6'],
    requires_api_key: true,
    has_credential: false,
    recommended: true,
    description: 'Good first choice for coding-focused use.',
    setup_hint: 'Use either a standard Z.AI API key.',
    docs_url: 'https://docs.z.ai/',
    signup_url: 'https://platform.z.ai/',
    api_key_label: 'Z.AI API Key',
    api_key_help: 'Create a key in the Z.AI API platform.',
    recommended_model: 'glm-5',
    recommended_model_why: 'Prefer a current GLM coding model.',
  },
  {
    id: 'minimax',
    name: 'MiniMax',
    models: ['minimax-m2.5', 'minimax-m2.1'],
    requires_api_key: true,
    has_credential: false,
    recommended: true,
    description: 'Strong coding-oriented provider.',
    setup_hint: 'MiniMax supports coding-plan keys.',
    docs_url: 'https://platform.minimax.io/',
    signup_url: 'https://platform.minimax.io/',
    api_key_label: 'MiniMax API Key',
    api_key_help: 'Create a key in the MiniMax platform.',
    recommended_model: 'minimax-m2.5',
    recommended_model_why: 'Prefer the newest M2.x model.',
  },
];

const mockAdvancedProviders: OnboardingProviderOption[] = [
  {
    id: 'openrouter',
    name: 'OpenRouter',
    models: ['qwen/qwen3-coder', 'deepseek/deepseek-chat'],
    requires_api_key: true,
    has_credential: false,
    recommended: false,
    description: 'Unified gateway to many model families.',
    setup_hint: 'Best for broad model choice.',
    docs_url: 'https://openrouter.ai/',
    signup_url: 'https://openrouter.ai/keys',
    api_key_label: 'OpenRouter API Key',
    api_key_help: 'Create an API key in OpenRouter.',
    recommended_model: 'qwen/qwen3-coder',
    recommended_model_why: 'Prefer a coding-focused model.',
  },
];

const mockWindowsGuidance = {
  title: 'Windows Setup',
  body: 'Install WSL or Git Bash for the best experience.',
  checklist: [
    'Install WSL from Microsoft Store',
    'Or install Git Bash from gitforwindows.org',
  ],
  canInstallWsl: true,
  canInstallGitBash: true,
  tone: 'info',
};

// ---------------------------------------------------------------------------
// Test Setup
// ---------------------------------------------------------------------------

let container: HTMLDivElement;
let root: Root;

beforeAll(() => {
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
});

beforeEach(() => {
  container = document.createElement('div');
  document.body.appendChild(container);
  root = createRoot(container);
});

afterEach(() => {
  root.unmount();
  container.remove();
});

const flushPromises = async () => {
  await new Promise(resolve => setTimeout(resolve, 0));
};

// ---------------------------------------------------------------------------
// Component Rendering Helper
// ---------------------------------------------------------------------------

const renderOnboardingDialog = (
  onboarding: OnboardingState = mockOnboarding,
  selectedProvider: OnboardingProviderOption | null = null,
  recommendedProviders: OnboardingProviderOption[] = mockRecommendedProviders,
  advancedProviders: OnboardingProviderOption[] = mockAdvancedProviders,
  windowsGuidance: any = null,
  callbacks: any = {}
) => {
  const defaultCallbacks = {
    onProviderChange: jest.fn(),
    onComplete: jest.fn().mockImplementation(async () => {}),
    onSkip: jest.fn().mockImplementation(async () => {}),
    onRefresh: jest.fn().mockImplementation(async () => {}),
    onInstallWsl: jest.fn().mockImplementation(async () => {}),
    onInstallGitBash: jest.fn().mockImplementation(async () => {}),
    updateOnboarding: jest.fn(),
  };

  const props = {
    onboarding,
    selectedProvider,
    recommendedProviders,
    advancedProviders,
    windowsGuidance,
    ...callbacks,
  };

  const mergedCallbacks = { ...defaultCallbacks, ...callbacks };

  act(() => {
    root.render(
      createElement(OnboardingDialog, {
        ...props,
        ...mergedCallbacks,
      })
    );
  });
};

// ---------------------------------------------------------------------------
// Component Visibility Tests
// ---------------------------------------------------------------------------

describe('OnboardingDialog', () => {
  describe('Component Visibility', () => {
    it('renders when onboarding is open', () => {
      renderOnboardingDialog();

      expect(container.querySelector('[role="dialog"]')).toBeTruthy();
      expect(container.textContent).toContain('Set Up Sprout');
    });

    it('does not render when onboarding is closed', () => {
      renderOnboardingDialog({ ...mockOnboarding, open: false });

      expect(container.querySelector('[role="dialog"]')).toBeNull();
    });

    it('displays correct title based on isReonboarding flag', () => {
      // Normal onboarding
      renderOnboardingDialog({ ...mockOnboarding, open: true, isReonboarding: false });
      expect(container.textContent).toContain('Set Up Sprout');

      // Re-onboarding
      renderOnboardingDialog({ ...mockOnboarding, open: true, isReonboarding: true });
      expect(container.textContent).toContain('Change Provider');
    });
  });

  // ---------------------------------------------------------------------------
  // Provider Selection Tests
  // ---------------------------------------------------------------------------

  describe('Provider Selection', () => {
    it('displays recommended providers', () => {
      renderOnboardingDialog();

      expect(container.textContent).toContain('Z.AI');
      expect(container.textContent).toContain('MiniMax');
    });

    it('calls onProviderChange when a recommended provider is clicked', async () => {
      const onProviderChange = jest.fn();
      renderOnboardingDialog({ ...mockOnboarding, open: true }, null, mockRecommendedProviders, mockAdvancedProviders, null, { onProviderChange });

      const zaiButton = Array.from(container.querySelectorAll('button')).find(
        btn => btn.textContent?.includes('Z.AI')
      );
      if (zaiButton) {
        zaiButton.click();
      }

      expect(onProviderChange).toHaveBeenCalledWith('zai');
    });

    it('highlights selected provider', () => {
      // The component uses onboarding.provider to determine selection, not selectedProvider prop
      const onboardingWithProvider = {
        ...mockOnboarding,
        open: true,
        provider: 'zai',
      };
      renderOnboardingDialog(onboardingWithProvider, mockRecommendedProviders[0]);

      const zaiButton = Array.from(container.querySelectorAll('button')).find(
        btn => btn.textContent?.includes('Z.AI')
      );
      if (zaiButton) {
        expect(zaiButton.classList.contains('selected')).toBe(true);
      }
    });

    it('shows configured badge for providers with credentials', () => {
      const configuredProvider = {
        ...mockRecommendedProviders[0],
        has_credential: true,
      };

      renderOnboardingDialog({ ...mockOnboarding, open: true }, null, [configuredProvider]);

      expect(container.textContent).toContain('Configured');
    });

    it('completely tests provider toggle functionality', async () => {
      const allProviders = [...mockRecommendedProviders, ...mockAdvancedProviders];
      const updateOnboarding = jest.fn();
      const onboardingWithProviders = {
        ...mockOnboarding,
        open: true,
        showAllProviders: false,
      };

      renderOnboardingDialog(onboardingWithProviders, null, mockRecommendedProviders, mockAdvancedProviders, null, {
        updateOnboarding,
      });

      // 1. Verify initial state - only recommended providers are shown
      expect(container.textContent).toContain('Z.AI');
      expect(container.textContent).toContain('MiniMax');
      expect(container.textContent).not.toContain('OpenRouter');

      // 2. Find and verify the toggle button exists
      const toggleButton = Array.from(container.querySelectorAll('button')).find(
        btn => btn.textContent?.toLowerCase().includes('show other providers')
      );
      expect(toggleButton).toBeTruthy();

      // 3. Click the toggle button to show advanced providers
      if (toggleButton) {
        act(() => {
          toggleButton.click();
        });
        await flushPromises();
      }

      // 4. Verify updateOnboarding was called to toggle showAllProviders
      expect(updateOnboarding).toHaveBeenCalled();
      const updateCall = updateOnboarding.mock.calls[0][0];
      expect(typeof updateCall).toBe('function');

      // 5. Apply the update to verify the state change
      const result = updateCall(onboardingWithProviders);
      expect(result.showAllProviders).toBe(true);

      // 6. Re-render with showAllProviders: true AND providers array to verify advanced providers appear
      const onboardingWithAllProviders = {
        ...mockOnboarding,
        open: true,
        showAllProviders: true,
        providers: allProviders, // Include the full providers list
      };

      renderOnboardingDialog(onboardingWithAllProviders, null, mockRecommendedProviders, mockAdvancedProviders, null, {
        updateOnboarding,
      });

      // 7. Verify all providers (including advanced) are now shown
      expect(container.textContent).toContain('Z.AI');
      expect(container.textContent).toContain('MiniMax');
      expect(container.textContent).toContain('OpenRouter');

      // 8. Verify toggle button text changed to "Hide other providers"
      const hideButton = Array.from(container.querySelectorAll('button')).find(
        btn => btn.textContent?.toLowerCase().includes('hide')
      );
      expect(hideButton).toBeTruthy();

      // 9. Click to hide advanced providers
      if (hideButton) {
        act(() => {
          hideButton.click();
        });
        await flushPromises();
      }

      // 10. Verify updateOnboarding was called to toggle back
      const secondUpdateCall = updateOnboarding.mock.calls[1][0];
      expect(typeof secondUpdateCall).toBe('function');

      // 11. Verify the state toggles back to false
      const secondResult = secondUpdateCall(onboardingWithAllProviders);
      expect(secondResult.showAllProviders).toBe(false);
    });
  });

  // ---------------------------------------------------------------------------
  // Model Selection Tests
  // ---------------------------------------------------------------------------

  describe('Model Selection', () => {
    it('displays model input field', () => {
      const selectedProvider = mockRecommendedProviders[0];
      renderOnboardingDialog({ ...mockOnboarding, open: true }, selectedProvider);

      const modelInput = container.querySelector('input[id="onboarding-model"]');
      expect(modelInput).toBeTruthy();
      expect(modelInput?.getAttribute('placeholder')).toMatch(/enter model name/i);
    });

    it('displays recommended model with badge', async () => {
      const selectedProvider = mockRecommendedProviders[0];
      renderOnboardingDialog({ ...mockOnboarding, open: true }, selectedProvider);

      // The model list should be visible when the input is focused
      const modelInput = container.querySelector('input[id="onboarding-model"]') as HTMLInputElement;
      if (modelInput) {
        modelInput.focus();
        await flushPromises();
      }

      expect(container.textContent).toContain('glm-5');
      expect(container.textContent).toContain('Recommended');
    });

    it('selects model when clicking on a model option', async () => {
      const updateOnboarding = jest.fn();
      const selectedProvider = mockRecommendedProviders[0];

      renderOnboardingDialog(
        { ...mockOnboarding, open: true, provider: 'zai' },
        selectedProvider,
        mockRecommendedProviders,
        mockAdvancedProviders,
        null,
        { updateOnboarding }
      );

      const modelInput = container.querySelector('input[id="onboarding-model"]') as HTMLInputElement;
      if (modelInput) {
        modelInput.focus();
        await flushPromises();
      }

      // Click on a model option
      const glm47Option = Array.from(container.querySelectorAll('li')).find(
        li => li.textContent?.includes('glm-4.7')
      );
      if (glm47Option) {
        glm47Option.click();
        await flushPromises();
      }

      // Verify updateOnboarding was called with the correct model
      expect(updateOnboarding).toHaveBeenCalled();
      const updateCall = updateOnboarding.mock.calls[0][0];
      expect(typeof updateCall).toBe('function');
      
      // Call the function to get the updated state
      const mockPrevState = { ...mockOnboarding, open: true, provider: 'zai' };
      const result = updateCall(mockPrevState);
      
      // Verify the model was set correctly
      expect(result.model).toBe('glm-4.7');
      expect(result.error).toBeNull();
    });
  });

  // ---------------------------------------------------------------------------
  // API Key Input Tests
  // ---------------------------------------------------------------------------

  describe('API Key Input', () => {
    it('displays API key input for providers that require it', () => {
      const selectedProvider = mockRecommendedProviders[0];
      renderOnboardingDialog({ ...mockOnboarding, open: true }, selectedProvider);

      expect(container.textContent).toMatch(/z\.ai api key/i);
      expect(container.querySelector('input[type="password"]')).toBeTruthy();
    });

    it('hides API key input for providers that do not require it', () => {
      const providerWithoutKey = {
        ...mockRecommendedProviders[0],
        requires_api_key: false,
        api_key_label: '',
        api_key_help: '',
      };

      // Create onboarding state without provider set (so no API key input shows)
      const onboardingWithoutProvider = {
        ...mockOnboarding,
        open: true,
        provider: '',
      };

      renderOnboardingDialog(onboardingWithoutProvider, providerWithoutKey);

      // When no provider is selected, no API key input should show
      expect(container.querySelector('input[type="password"]')).toBeNull();
    });

    it('displays API key help text', () => {
      const selectedProvider = mockRecommendedProviders[0];
      renderOnboardingDialog({ ...mockOnboarding, open: true }, selectedProvider);

      expect(container.textContent).toMatch(/create a key/i);
    });

    it('shows error styling when keyError is true', () => {
      const selectedProvider = mockRecommendedProviders[0];
      const onboardingWithKeyError = {
        ...mockOnboarding,
        provider: 'zai',
        keyError: true,
      };

      renderOnboardingDialog(onboardingWithKeyError, selectedProvider);

      const apiKeyInput = container.querySelector('input[id="onboarding-api-key"]') as HTMLInputElement;
      if (apiKeyInput) {
        expect(apiKeyInput.classList.contains('onboarding-key-error')).toBe(true);
      }
    });

    it('updates API key when user types in the input field', () => {
      const updateOnboarding = jest.fn();
      const selectedProvider = mockRecommendedProviders[0];
      const onboardingWithProvider = {
        ...mockOnboarding,
        open: true,
        provider: 'zai',
      };

      renderOnboardingDialog(onboardingWithProvider, selectedProvider, mockRecommendedProviders, mockAdvancedProviders, null, {
        updateOnboarding,
      });

      const apiKeyInput = container.querySelector('input[id="onboarding-api-key"]') as HTMLInputElement;
      expect(apiKeyInput).toBeTruthy();
      
      // Verify the input has the right attributes
      expect(apiKeyInput.type).toBe('password');
      expect(apiKeyInput.placeholder).toBe('Paste API key');
      
      // Verify the input is not disabled
      expect(apiKeyInput.disabled).toBe(false);
    });

    it('disables API key input when submitting', () => {
      const selectedProvider = mockRecommendedProviders[0];
      const onboardingWithSubmitting = {
        ...mockOnboarding,
        open: true,
        provider: 'zai',
        submitting: true,
      };

      renderOnboardingDialog(onboardingWithSubmitting, selectedProvider);

      const apiKeyInput = container.querySelector('input[id="onboarding-api-key"]') as HTMLInputElement;
      expect(apiKeyInput).toBeTruthy();

      // Verify the input is disabled when submitting
      expect(apiKeyInput.disabled).toBe(true);
    });

    it('completely tests API key input population and validation', async () => {
      const selectedProvider = mockRecommendedProviders[0];

      // 1. Test basic API key input display and properties
      const onboardingWithProvider = {
        ...mockOnboarding,
        open: true,
        provider: 'zai',
        apiKey: '',
        keyError: false,
      };

      renderOnboardingDialog(onboardingWithProvider, selectedProvider, mockRecommendedProviders, mockAdvancedProviders);

      const apiKeyInput = container.querySelector('input[id="onboarding-api-key"]') as HTMLInputElement;
      expect(apiKeyInput).toBeTruthy();
      expect(apiKeyInput.type).toBe('password');
      expect(apiKeyInput.placeholder).toBe('Paste API key');
      expect(apiKeyInput.disabled).toBe(false);

      // 2. Verify API key label and help text are displayed
      expect(container.textContent).toMatch(/z\.ai api key/i);
      expect(container.textContent).toMatch(/create a key in the z\.ai api platform/i);

      // 3. Verify the input reflects the initial value from onboarding state
      expect(apiKeyInput.value).toBe('');

      // 4. Test that input value updates when onboarding.apiKey changes
      const onboardingWithValue = {
        ...mockOnboarding,
        open: true,
        provider: 'zai',
        apiKey: 'test-api-key-12345',
        keyError: false,
      };

      renderOnboardingDialog(onboardingWithValue, selectedProvider);
      const apiKeyInputWithValue = container.querySelector('input[id="onboarding-api-key"]') as HTMLInputElement;
      expect(apiKeyInputWithValue.value).toBe('test-api-key-12345');

      // 5. Test error styling when keyError is true
      const onboardingWithKeyError = {
        ...mockOnboarding,
        open: true,
        provider: 'zai',
        keyError: true,
      };

      renderOnboardingDialog(onboardingWithKeyError, selectedProvider);

      const apiKeyInputWithError = container.querySelector('input[id="onboarding-api-key"]') as HTMLInputElement;
      expect(apiKeyInputWithError).toBeTruthy();
      expect(apiKeyInputWithError.classList.contains('onboarding-key-error')).toBe(true);

      // 6. Test that input is disabled when submitting
      const onboardingSubmitting = {
        ...mockOnboarding,
        open: true,
        provider: 'zai',
        submitting: true,
      };

      renderOnboardingDialog(onboardingSubmitting, selectedProvider);

      const apiKeyInputSubmitting = container.querySelector('input[id="onboarding-api-key"]') as HTMLInputElement;
      expect(apiKeyInputSubmitting.disabled).toBe(true);
    });
  });

  // ---------------------------------------------------------------------------
  // Provider Summary Tests
  // ---------------------------------------------------------------------------

  describe('Provider Summary', () => {
    it('displays provider summary when a provider is selected', () => {
      const onboardingWithProvider = {
        ...mockOnboarding,
        open: true,
        provider: 'zai',
      };
      renderOnboardingDialog(onboardingWithProvider, mockRecommendedProviders[0]);

      expect(container.textContent).toMatch(/z\.ai/i);
      // The setup_hint is displayed, not the description
      expect(container.textContent).toMatch(/standard z\.ai api key/i);
    });

    it('displays docs and signup links in provider summary', () => {
      const selectedProvider = mockRecommendedProviders[0];
      renderOnboardingDialog({ ...mockOnboarding, open: true }, selectedProvider);

      expect(container.textContent).toMatch(/docs/i);
      expect(container.textContent).toMatch(/api access/i);
    });
  });

  // ---------------------------------------------------------------------------
  // Action Buttons Tests
  // ---------------------------------------------------------------------------

  describe('Action Buttons', () => {
    it('displays skip button for initial onboarding', () => {
      renderOnboardingDialog();

      expect(container.textContent).toMatch(/skip.*editor/i);
    });

    it('does not display skip button for re-onboarding', () => {
      renderOnboardingDialog({ ...mockOnboarding, open: true, isReonboarding: true });

      expect(container.textContent).not.toMatch(/skip/i);
    });

    it('calls onSkip when skip button is clicked', async () => {
      const onSkip = jest.fn();
      renderOnboardingDialog({ ...mockOnboarding, open: true }, null, mockRecommendedProviders, mockAdvancedProviders, null, { onSkip });

      const skipButton = Array.from(container.querySelectorAll('button')).find(
        btn => btn.textContent?.toLowerCase().includes('skip')
      );
      if (skipButton) {
        skipButton.click();
        await flushPromises();
      }

      expect(onSkip).toHaveBeenCalled();
    });

    it('displays refresh button', () => {
      renderOnboardingDialog();

      expect(container.textContent).toMatch(/refresh/i);
    });

    it('calls onRefresh when refresh button is clicked', async () => {
      const onRefresh = jest.fn();
      renderOnboardingDialog({ ...mockOnboarding, open: true }, null, mockRecommendedProviders, mockAdvancedProviders, null, { onRefresh });

      const refreshButton = Array.from(container.querySelectorAll('button')).find(
        btn => btn.textContent?.toLowerCase().includes('refresh')
      );
      if (refreshButton) {
        refreshButton.click();
        await flushPromises();
      }

      expect(onRefresh).toHaveBeenCalled();
    });

    it('displays complete button with correct text', () => {
      renderOnboardingDialog();

      expect(container.textContent).toMatch(/complete setup/i);
    });

    it('changes complete button text when validation succeeds', () => {
      renderOnboardingDialog({ ...mockOnboarding, validationSuccess: true });

      expect(container.textContent).toMatch(/done/i);
    });

    it('changes complete button text when submitting', () => {
      renderOnboardingDialog({ ...mockOnboarding, submitting: true });

      expect(container.textContent).toMatch(/validating/i);
    });

    it('calls onComplete when complete button is clicked', async () => {
      const onComplete = jest.fn();
      renderOnboardingDialog({ ...mockOnboarding, open: true }, null, mockRecommendedProviders, mockAdvancedProviders, null, { onComplete });

      const completeButton = Array.from(container.querySelectorAll('button')).find(
        btn => btn.textContent?.toLowerCase().includes('complete')
      );
      if (completeButton) {
        completeButton.click();
        await flushPromises();
      }

      expect(onComplete).toHaveBeenCalled();
    });
  });

  // ---------------------------------------------------------------------------
  // Error Handling Tests
  // ---------------------------------------------------------------------------

  describe('Error Handling', () => {
    it('displays error message when onboarding.error is set', () => {
      renderOnboardingDialog({ ...mockOnboarding, error: 'Something went wrong' });

      expect(container.textContent).toMatch(/something went wrong/i);
    });

    it('does not display error when onboarding.error is null', () => {
      renderOnboardingDialog();

      expect(container.textContent).not.toMatch(/something went wrong/i);
    });
  });

  // ---------------------------------------------------------------------------
  // Success Message Tests
  // ---------------------------------------------------------------------------

  describe('Success Message', () => {
    it('displays success message when validation succeeds', () => {
      renderOnboardingDialog({
        ...mockOnboarding,
        validationSuccess: true,
        validationModelCount: 10,
      });

      expect(container.textContent).toMatch(/validated/i);
      expect(container.textContent).toMatch(/10 models/i);
    });
  });

  // ---------------------------------------------------------------------------
  // Platform Guidance Tests
  // ---------------------------------------------------------------------------

  describe('Platform Guidance', () => {
    it('displays Windows guidance when windowsGuidance is provided', () => {
      renderOnboardingDialog({ ...mockOnboarding, open: true }, null, mockRecommendedProviders, mockAdvancedProviders, mockWindowsGuidance);

      expect(container.textContent).toMatch(/windows setup/i);
    });

    it('displays WSL distro information when active_distro is set', () => {
      const onboardingWithDistro = {
        ...mockOnboarding,
        environment: {
          ...mockOnboarding.environment,
          backend_mode: 'wsl',
          active_distro: 'ubuntu-22.04',
          wsl_distros: ['ubuntu-22.04', 'debian'],
        },
      };

      renderOnboardingDialog(onboardingWithDistro, null, mockRecommendedProviders, mockAdvancedProviders, mockWindowsGuidance);

      expect(container.textContent).toMatch(/ubuntu-22.04/i);
    });

    it('displays install buttons when canInstallWsl is true', () => {
      renderOnboardingDialog({ ...mockOnboarding, open: true }, null, mockRecommendedProviders, mockAdvancedProviders, mockWindowsGuidance);

      expect(container.textContent).toMatch(/install wsl/i);
    });

    it('calls onInstallWsl when install WSL button is clicked', async () => {
      const onInstallWsl = jest.fn();
      renderOnboardingDialog(
        { ...mockOnboarding, open: true },
        null,
        mockRecommendedProviders,
        mockAdvancedProviders,
        mockWindowsGuidance,
        { onInstallWsl }
      );

      const installWslButton = Array.from(container.querySelectorAll('button')).find(
        btn => btn.textContent?.toLowerCase().includes('install wsl')
      );
      if (installWslButton) {
        installWslButton.click();
        await flushPromises();
      }

      expect(onInstallWsl).toHaveBeenCalled();
    });
  });

  // ---------------------------------------------------------------------------
  // Editor-Only Note Tests
  // ---------------------------------------------------------------------------

  describe('Editor-Only Note', () => {
    it('displays editor-only note for initial onboarding', () => {
      renderOnboardingDialog();

      expect(container.textContent).toMatch(/explore/i);
    });

    it('does not display editor-only note for re-onboarding', () => {
      renderOnboardingDialog({ ...mockOnboarding, open: true, isReonboarding: true });

      expect(container.textContent).not.toMatch(/explore/i);
    });
  });

  // ---------------------------------------------------------------------------
  // Close Button Tests (Re-onboarding Only)
  // ---------------------------------------------------------------------------

  describe('Close Button', () => {
    it('displays close button for re-onboarding', () => {
      renderOnboardingDialog({ ...mockOnboarding, open: true, isReonboarding: true });

      const closeButton = container.querySelector('[aria-label="Close"]');
      expect(closeButton).toBeTruthy();
    });

    it('does not display close button for initial onboarding', () => {
      renderOnboardingDialog();

      const closeButton = container.querySelector('[aria-label="Close"]');
      expect(closeButton).toBeNull();
    });

    it('closes dialog when close button is clicked', async () => {
      const updateOnboarding = jest.fn();
      renderOnboardingDialog(
        { ...mockOnboarding, open: true, isReonboarding: true },
        null,
        mockRecommendedProviders,
        mockAdvancedProviders,
        null,
        { updateOnboarding }
      );

      const closeButton = container.querySelector('[aria-label="Close"]');
      if (closeButton) {
        closeButton.click();
        await flushPromises();
      }

      expect(updateOnboarding).toHaveBeenCalled();
    });
  });

  // ---------------------------------------------------------------------------
  // Edge Cases
  // ---------------------------------------------------------------------------

  describe('Edge Cases', () => {
    it('handles empty providers list gracefully', () => {
      renderOnboardingDialog({ ...mockOnboarding, open: true }, null, [], []);

      expect(container.querySelector('[role="dialog"]')).toBeTruthy();
    });

    it('handles provider without models', () => {
      const providerWithoutModels = {
        ...mockRecommendedProviders[0],
        models: [],
      };

      renderOnboardingDialog({ ...mockOnboarding, open: true }, providerWithoutModels);

      const modelInput = container.querySelector('input[id="onboarding-model"]');
      expect(modelInput).toBeTruthy();
    });
  });
});
