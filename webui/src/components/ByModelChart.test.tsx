import { render, screen } from '@testing-library/react';
import ByModelChart from './ByModelChart';

vi.mock('./ByModelChart.css', () => ({}));

describe('ByModelChart', () => {
  it('renders rows sorted by cost descending', () => {
    const byModel = {
      'openai:gpt-3.5': 0.05,
      'anthropic:claude': 0.2,
      'openai:gpt-4': 0.15,
    };
    render(<ByModelChart byModel={byModel} />);

    expect(screen.getByTestId('by-model-chart')).toBeInTheDocument();
    expect(screen.getByTestId('by-model-row-0')).toHaveTextContent('anthropic:claude');
    expect(screen.getByTestId('by-model-row-1')).toHaveTextContent('openai:gpt-4');
    expect(screen.getByTestId('by-model-row-2')).toHaveTextContent('openai:gpt-3.5');
  });

  it('renders dollar values for each model', () => {
    const byModel = {
      'openai:gpt-4': 1.2345,
      'anthropic:claude': 2.5678,
    };
    render(<ByModelChart byModel={byModel} />);

    expect(screen.getByTestId('by-model-row-0')).toHaveTextContent('$2.5678');
    expect(screen.getByTestId('by-model-row-1')).toHaveTextContent('$1.2345');
  });

  it('empty state when no data', () => {
    render(<ByModelChart byModel={{}} />);
    expect(screen.getByTestId('by-model-empty')).toBeInTheDocument();
  });

  it('loading state shows skeleton rows', () => {
    render(<ByModelChart byModel={{}} loading={true} />);
    const skeletonRows = document.querySelectorAll('.by-model-row--skeleton');
    expect(skeletonRows).toHaveLength(5);
  });

  it('error state shows error message', () => {
    render(<ByModelChart byModel={{}} error="chart failed" />);
    const chart = screen.getByTestId('by-model-chart');
    expect(chart).toHaveTextContent('chart failed');
    expect(chart).toHaveClass('by-model-chart--error');
  });

  it('bars have proportional widths', () => {
    const byModel = {
      'model-a': 1,
      'model-b': 3,
      'model-c': 6,
    };
    render(<ByModelChart byModel={byModel} />);

    const bars = document.querySelectorAll('.by-model-bar:not(.by-model-bar--skeleton)');
    expect(bars).toHaveLength(3);

    // Bars are sorted by cost descending: model-c (6), model-b (3), model-a (1)
    const widthC = parseFloat(bars[0].style.width); // model-c, highest
    const widthB = parseFloat(bars[1].style.width); // model-b
    const widthA = parseFloat(bars[2].style.width); // model-a, lowest

    // model-c has the highest cost (6), so it should be 100%
    expect(widthC).toBeCloseTo(100, 2);
    // model-b is half of model-c (3/6 = 50%)
    expect(widthB).toBeCloseTo(50, 2);
    // model-a is 1/6 of model-c
    expect(widthA).toBeCloseTo(100 / 6, 1);
  });

  it('cycles colors through the palette', () => {
    const byModel: Record<string, number> = {};
    for (let i = 0; i < 10; i++) {
      byModel[`model-${i}`] = 1;
    }
    render(<ByModelChart byModel={byModel} />);

    const bars = document.querySelectorAll('.by-model-bar:not(.by-model-bar--skeleton)');
    expect(bars).toHaveLength(10);

    // First two bars should have different colors
    const color0 = bars[0].style.backgroundColor;
    const color1 = bars[1].style.backgroundColor;
    expect(color0).not.toBe(color1);

    // 8th bar should cycle back to the same color as 1st
    const color7 = bars[7].style.backgroundColor;
    expect(color7).toBe(color0);
  });

  it('truncates long model names in labels', () => {
    const byModel = {
      'very-long-model-name-that-should-be-truncated-in-the-ui': 1.0,
    };
    render(<ByModelChart byModel={byModel} />);

    const label = screen.getByTestId('by-model-row-0').querySelector('.by-model-label');
    expect(label).toBeInTheDocument();
    expect(label!.textContent!.length).toBeLessThan(25);
  });

  it('long model names have full name in title attribute', () => {
    const byModel = {
      'very-long-model-name-that-should-be-truncated-in-the-ui': 1.0,
    };
    render(<ByModelChart byModel={byModel} />);

    const label = screen.getByTestId('by-model-row-0').querySelector('.by-model-label');
    expect(label?.getAttribute('title')).toBe('very-long-model-name-that-should-be-truncated-in-the-ui');
  });
});
