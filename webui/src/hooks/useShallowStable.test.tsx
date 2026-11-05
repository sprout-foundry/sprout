import { act, useState } from 'react';
import { createRoot } from 'react-dom/client';
import { useShallowStable } from './useShallowStable';

interface Captured {
  identity: () => object;
  render: () => void;
}

function Harness({ source, captured }: { source: () => Record<string, unknown>; captured: Captured }) {
  const [, setTick] = useState(0);
  const value = useShallowStable(source());
  captured.identity = () => value;
  captured.render = () => setTick((n) => n + 1);
  return null;
}

function mount(source: () => Record<string, unknown>): { captured: Captured; unmount: () => void } {
  const captured = {} as Captured;
  const container = document.createElement('div');
  document.body.appendChild(container);
  const root = createRoot(container);
  act(() => {
    root.render(<Harness source={source} captured={captured} />);
  });
  return {
    captured,
    unmount: () => {
      act(() => root.unmount());
      container.remove();
    },
  };
}

describe('useShallowStable', () => {
  it('keeps the identity when every value is unchanged', () => {
    const stable = { a: 1, fn: () => undefined };
    const view = mount(() => ({ ...stable }));
    const first = view.captured.identity();

    act(() => view.captured.render());

    expect(view.captured.identity()).toBe(first);
    view.unmount();
  });

  it('replaces the identity when a value changes', () => {
    let n = 0;
    const view = mount(() => ({ n }));
    const first = view.captured.identity();

    n = 1;
    act(() => view.captured.render());

    expect(view.captured.identity()).not.toBe(first);
    expect((view.captured.identity() as { n: number }).n).toBe(1);
    view.unmount();
  });

  it('replaces the identity when the key set changes', () => {
    let extra = false;
    const view = mount(() => (extra ? { a: 1, b: 2 } : { a: 1 }));
    const first = view.captured.identity();

    extra = true;
    act(() => view.captured.render());

    expect(view.captured.identity()).not.toBe(first);
    view.unmount();
  });
});
