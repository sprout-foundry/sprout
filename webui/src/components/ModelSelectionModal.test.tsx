import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import ModelSelectionModal from './ModelSelectionModal';
import { ApiService } from '../services/api';
import { debugLog } from '../utils/log';

// Mock dependencies
vi.mock('../services/api');
vi.mock('../utils/log');

describe('ModelSelectionModal', () => {
  const mockApiService = {
    getInstance: vi.fn(),
  };

  const mockProvider = 'openai';
  const mockOnClose = vi.fn();
  const mockOnSelectModel = vi.fn();

  beforeEach(() => {
    vi.clearAllMocks();
    (ApiService.getInstance as vi.Mock).mockReturnValue(mockApiService);
    mockApiService.getProviderModels = vi.fn();
  });

  afterEach(() => {
    // Reset body overflow after each test
    document.body.style.overflow = '';
  });

  it('renders loading state initially', () => {
    mockApiService.getProviderModels.mockImplementation(
      () => new Promise(() => {}), // Never resolves
    );

    render(<ModelSelectionModal provider={mockProvider} onClose={mockOnClose} onSelectModel={mockOnSelectModel} />);

    expect(screen.getByText('Model Not Available')).toBeInTheDocument();
    expect(screen.getByText(/configured model is not available/i)).toBeInTheDocument();
    expect(screen.getByText('Loading available models...')).toBeInTheDocument();
  });

  it('renders list of models after successful fetch', async () => {
    const mockModels = ['gpt-4o', 'gpt-4o-mini', 'gpt-3.5-turbo'];
    mockApiService.getProviderModels.mockResolvedValue({
      provider: mockProvider,
      models: mockModels,
    });

    render(<ModelSelectionModal provider={mockProvider} onClose={mockOnClose} onSelectModel={mockOnSelectModel} />);

    await waitFor(() => {
      expect(screen.queryByText('Loading available models...')).not.toBeInTheDocument();
    });

    expect(screen.getByText('gpt-4o')).toBeInTheDocument();
    expect(screen.getByText('gpt-4o-mini')).toBeInTheDocument();
    expect(screen.getByText('gpt-3.5-turbo')).toBeInTheDocument();
  });

  it('renders error message when fetch fails', async () => {
    const errorMessage = 'Failed to fetch models';
    mockApiService.getProviderModels.mockRejectedValue(new Error(errorMessage));

    render(<ModelSelectionModal provider={mockProvider} onClose={mockOnClose} onSelectModel={mockOnSelectModel} />);

    await waitFor(() => {
      expect(screen.queryByText('Loading available models...')).not.toBeInTheDocument();
    });

    expect(screen.getByText(errorMessage)).toBeInTheDocument();
  });

  it('renders empty state when no models available', async () => {
    mockApiService.getProviderModels.mockResolvedValue({
      provider: mockProvider,
      models: [],
    });

    render(<ModelSelectionModal provider={mockProvider} onClose={mockOnClose} onSelectModel={mockOnSelectModel} />);

    await waitFor(() => {
      expect(screen.queryByText('Loading available models...')).not.toBeInTheDocument();
    });

    expect(screen.getByText('No models available for this provider.')).toBeInTheDocument();
  });

  it('closes when Cancel button is clicked', async () => {
    mockApiService.getProviderModels.mockResolvedValue({
      provider: mockProvider,
      models: ['gpt-4o'],
    });

    render(<ModelSelectionModal provider={mockProvider} onClose={mockOnClose} onSelectModel={mockOnSelectModel} />);

    await waitFor(() => {
      expect(screen.getByText('Cancel')).toBeInTheDocument();
    });

    fireEvent.click(screen.getByText('Cancel'));
    expect(mockOnClose).toHaveBeenCalledTimes(1);
  });

  it('selects model and calls onSelectModel when Select Model button is clicked', async () => {
    const mockModels = ['gpt-4o', 'gpt-4o-mini'];
    mockApiService.getProviderModels.mockResolvedValue({
      provider: mockProvider,
      models: mockModels,
    });

    render(<ModelSelectionModal provider={mockProvider} onClose={mockOnClose} onSelectModel={mockOnSelectModel} />);

    await waitFor(() => {
      expect(screen.getByText('Select Model')).toBeInTheDocument();
    });

    // First model should be auto-selected
    expect(screen.getByText('gpt-4o')).toBeInTheDocument();

    // Click select button
    fireEvent.click(screen.getByText('Select Model'));
    expect(mockOnSelectModel).toHaveBeenCalledWith('gpt-4o');
  });

  it('updates selection when clicking on a different model', async () => {
    const mockModels = ['gpt-4o', 'gpt-4o-mini'];
    mockApiService.getProviderModels.mockResolvedValue({
      provider: mockProvider,
      models: mockModels,
    });

    render(<ModelSelectionModal provider={mockProvider} onClose={mockOnClose} onSelectModel={mockOnSelectModel} />);

    await waitFor(() => {
      expect(screen.getByText('gpt-4o')).toBeInTheDocument();
    });

    // Click on second model
    fireEvent.click(screen.getByText('gpt-4o-mini'));

    // Click select button
    fireEvent.click(screen.getByText('Select Model'));
    expect(mockOnSelectModel).toHaveBeenCalledWith('gpt-4o-mini');
  });

  it('closes when Escape key is pressed', async () => {
    mockApiService.getProviderModels.mockResolvedValue({
      provider: mockProvider,
      models: ['gpt-4o'],
    });

    render(<ModelSelectionModal provider={mockProvider} onClose={mockOnClose} onSelectModel={mockOnSelectModel} />);

    await waitFor(() => {
      expect(screen.getByText('Cancel')).toBeInTheDocument();
    });

    fireEvent.keyDown(document, { key: 'Escape' });
    expect(mockOnClose).toHaveBeenCalledTimes(1);
  });

  it('disables Select Model button when no model is selected', async () => {
    // Empty models array means no selection
    mockApiService.getProviderModels.mockResolvedValue({
      provider: mockProvider,
      models: [],
    });

    render(<ModelSelectionModal provider={mockProvider} onClose={mockOnClose} onSelectModel={mockOnSelectModel} />);

    await waitFor(() => {
      expect(screen.getByText('Select Model')).toBeInTheDocument();
    });

    expect(screen.getByText('Select Model')).toBeDisabled();
  });
});
