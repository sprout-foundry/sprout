/**
 * Onboarding API types.
 */

export interface OnboardingProviderOption {
  id: string;
  name: string;
  models: string[];
  requires_api_key: boolean;
  has_credential: boolean;
  recommended: boolean;
  description: string;
  setup_hint: string;
  docs_url: string;
  signup_url: string;
  api_key_label: string;
  api_key_help: string;
  recommended_model: string;
  recommended_model_why: string;
}

export interface OnboardingEnvironment {
  runtime_platform: string;
  host_platform: string;
  backend_mode: string;
  has_wsl: boolean;
  has_git_bash: boolean;
  recommended_terminal: string;
  active_distro: string;
  wsl_distros: string[];
}

export interface OnboardingStatusResponse {
  setup_required: boolean;
  reason: string;
  current_provider: string;
  current_model: string;
  providers: OnboardingProviderOption[];
  environment?: OnboardingEnvironment;
}

export interface CompleteOnboardingRequest {
  provider: string;
  model?: string;
  api_key?: string;
}

export interface CompleteOnboardingResponse {
  success: boolean;
  message: string;
  provider: string;
  model: string;
  validation?: { tested: boolean; model_count?: number };
}
