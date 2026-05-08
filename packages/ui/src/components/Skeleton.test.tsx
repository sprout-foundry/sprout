import { render, screen, cleanup, within } from '@testing-library/react';
import { Skeleton, SkeletonText } from './Skeleton';

// Clean up after each test
afterEach(() => {
  cleanup();
});

describe('Skeleton Component', () => {
  it('renders with default props', () => {
    const { container } = render(<Skeleton />);
    const skeleton = container.querySelector('.skeleton');
    expect(skeleton).toBeInTheDocument();
    expect(skeleton).toHaveClass('skeleton');
  });

  it('renders with custom width and height', () => {
    const { container } = render(<Skeleton width="100px" height="20px" />);
    const skeleton = container.querySelector('.skeleton');
    expect(skeleton).toBeInTheDocument();
    expect(skeleton).toHaveStyle({
      width: '100px',
      height: '20px',
    });
  });

  it('renders with numeric width and height', () => {
    const { container } = render(<Skeleton width={200} height={30} />);
    const skeleton = container.querySelector('.skeleton');
    expect(skeleton).toBeInTheDocument();
    expect(skeleton).toHaveStyle({
      width: '200px',
      height: '30px',
    });
  });

  it('renders with custom radius', () => {
    const { container } = render(<Skeleton radius="8px" />);
    const skeleton = container.querySelector('.skeleton');
    expect(skeleton).toBeInTheDocument();
    expect(skeleton).toHaveStyle({
      borderRadius: '8px',
    });
  });

  it('renders with custom className', () => {
    const { container } = render(<Skeleton className="custom-class" />);
    const skeleton = container.querySelector('.skeleton');
    expect(skeleton).toBeInTheDocument();
    expect(skeleton).toHaveClass('skeleton', 'custom-class');
  });

  it('renders with inline styles', () => {
    const { container } = render(<Skeleton style={{ margin: '10px' }} />);
    const skeleton = container.querySelector('.skeleton');
    expect(skeleton).toBeInTheDocument();
    expect(skeleton).toHaveStyle({ margin: '10px' });
  });
});

describe('SkeletonText Component', () => {
  it('renders with default 3 lines', () => {
    const { container } = render(<SkeletonText />);
    const skeletonContainer = container.querySelector('.skeleton-text');
    expect(skeletonContainer).toBeInTheDocument();
    expect(skeletonContainer).toHaveClass('skeleton-text');
    expect(skeletonContainer?.children).toHaveLength(3);
  });

  it('renders with custom line count', () => {
    const { container } = render(<SkeletonText lines={5} />);
    const skeletonContainer = container.querySelector('.skeleton-text');
    expect(skeletonContainer).toBeInTheDocument();
    expect(skeletonContainer?.children).toHaveLength(5);
  });

  it('renders lines with varying widths', () => {
    const { container } = render(<SkeletonText lines={4} lastLineWidth="40%" />);
    const skeletonContainer = container.querySelector('.skeleton-text');
    expect(skeletonContainer).toBeInTheDocument();
    const skeletons = skeletonContainer?.querySelectorAll('.skeleton');
    expect(skeletons).toHaveLength(4);
    // Last line should have 40% width
    if (skeletons && skeletons[3]) {
      expect(skeletons[3]).toHaveStyle({ width: '40%' });
    }
  });

  it('renders with custom gap and line height', () => {
    const { container } = render(<SkeletonText gap="12px" lineHeight="16px" />);
    const skeletonContainer = container.querySelector('.skeleton-text');
    expect(skeletonContainer).toBeInTheDocument();
    expect(skeletonContainer).toHaveStyle({
      gap: '12px',
    });
    const skeletons = skeletonContainer?.querySelectorAll('.skeleton');
    skeletons?.forEach((skeleton) => {
      expect(skeleton).toHaveStyle({ height: '16px' });
    });
  });

  it('renders with custom className', () => {
    const { container } = render(<SkeletonText className="custom-text-class" />);
    const skeletonContainer = container.querySelector('.skeleton-text');
    expect(skeletonContainer).toBeInTheDocument();
    expect(skeletonContainer).toHaveClass('skeleton-text', 'custom-text-class');
  });
});
