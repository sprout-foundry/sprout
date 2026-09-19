/**
 * SP-140-3 item 3.9 — feedback write path (detail-pane affordance).
 *
 * Covers the §3e contract: with an asset selected the pane renders the
 * annotation affordance, submitting a note calls `designApi.writeFeedback`
 * with the SP-140-4d schema shape (target / status / resolution / annotations,
 * with the per-annotation `resolved` and top-level `resolution` fields
 * present), and the written path resolves under `design/feedback/`.
 *
 * `designApi` is mocked (the `writeFeedback` call is the assertion target)
 * while the real pure `feedbackWrite` model stays in play, per the
 * ScreensGrid.test / FlowsCanvas.test convention. Assertions use the suite's
 * plain-expect style rather than `@testing-library/jest-dom` matchers.
 *
 * Not this item: drawing annotations on a rendered screen, marking them
 * resolved, and the agent-side consumption of the file are SP-140-4.
 */

import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { SproutAdapterProvider } from '../../contexts/SproutAdapterContext';
import { writeFeedback } from '../../services/api/designApi';
import type { DesignFeedbackFile } from '../../services/api/types';
import DesignDetailPane from './DesignDetailPane';
import DesignFeedbackAffordance from './DesignFeedbackAffordance';

vi.mock('../../services/api/designApi', async () => {
  const actual = await vi.importActual<Record<string, unknown>>('../../services/api/designApi');
  return { ...actual, writeFeedback: vi.fn() };
});

const mockedWriteFeedback = vi.mocked(writeFeedback);

const FIXED_NOW = '2026-09-15T10:36:47Z';

/** Render the affordance with a selected asset and a mocked write transport. */
function renderAffordance(props: Partial<React.ComponentProps<typeof DesignFeedbackAffordance>> = {}) {
  return render(
    <SproutAdapterProvider>
      <DesignFeedbackAffordance path="design/screens/login.html" now={() => FIXED_NOW} {...props} />
    </SproutAdapterProvider>,
  );
}

/** Fill the note (and optionally the area) and submit the form. */
function submitNote(note: string, area?: string) {
  if (!screen.queryByTestId('design-feedback-form')) {
    fireEvent.click(screen.getByTestId('design-feedback-add'));
  }
  if (area) fireEvent.change(screen.getByTestId('design-feedback-area'), { target: { value: area } });
  fireEvent.change(screen.getByTestId('design-feedback-note'), { target: { value: note } });
  fireEvent.click(screen.getByTestId('design-feedback-submit'));
}

/** The `DesignFeedbackFile` handed to the mocked write on its last call. */
function writtenJson(): DesignFeedbackFile {
  return mockedWriteFeedback.mock.calls.at(-1)![2];
}

beforeEach(() => {
  mockedWriteFeedback.mockReset();
  mockedWriteFeedback.mockResolvedValue({
    path: 'design/feedback/login.json',
    content: '{}',
    response: { ok: true, status: 200 } as Response,
  });
});

describe('feedback affordance rendering', () => {
  it('renders the affordance when an asset is selected', () => {
    renderAffordance();
    const affordance = screen.getByTestId('design-feedback-affordance');
    expect(affordance.getAttribute('data-target')).toBe('design/screens/login.html');
    expect(screen.getByTestId('design-feedback-add').textContent).toBe('Add feedback');
  });

  it('renders nothing without a selected asset', () => {
    render(
      <SproutAdapterProvider>
        <DesignFeedbackAffordance path={null} />
      </SproutAdapterProvider>,
    );
    expect(screen.queryByTestId('design-feedback-affordance')).toBeNull();
  });

  it('normalizes a design-relative selection to the workspace-relative target', () => {
    renderAffordance({ path: 'screens/login.html' });
    expect(screen.getByTestId('design-feedback-affordance').getAttribute('data-target')).toBe(
      'design/screens/login.html',
    );
  });

  it('reveals the note/area form when the affordance is opened', () => {
    renderAffordance();
    expect(screen.queryByTestId('design-feedback-form')).toBeNull();
    fireEvent.click(screen.getByTestId('design-feedback-add'));
    expect(screen.getByTestId('design-feedback-form')).toBeTruthy();
    expect(screen.getByTestId('design-feedback-target').textContent).toBe('design/screens/login.html');
  });

  it('disables submit until a note is entered', () => {
    renderAffordance();
    fireEvent.click(screen.getByTestId('design-feedback-add'));
    const submit = screen.getByTestId('design-feedback-submit') as HTMLButtonElement;
    expect(submit.disabled).toBe(true);
    fireEvent.change(screen.getByTestId('design-feedback-note'), { target: { value: 'hi' } });
    expect((screen.getByTestId('design-feedback-submit') as HTMLButtonElement).disabled).toBe(false);
  });
});

describe('feedback write path', () => {
  it('writes the SP-140-4d schema for the selected asset on submit', async () => {
    renderAffordance();
    submitNote('Primary CTA reads as secondary', 'hierarchy');

    await waitFor(() => expect(mockedWriteFeedback).toHaveBeenCalledTimes(1));

    const [transport, target, json] = mockedWriteFeedback.mock.calls[0];
    expect(transport).toBeInstanceOf(Function);
    expect(target).toBe('design/screens/login.html');
    // Every §4d field is present on the document.
    expect(Object.keys(json).sort()).toEqual(['annotations', 'resolution', 'status', 'target']);
    expect(json.target).toBe('design/screens/login.html');
    expect(json.status).toBe('changes-requested');
    expect(json.resolution).toBe('');
    expect(json.annotations).toHaveLength(1);

    const [annotation] = json.annotations;
    expect(Object.keys(annotation).sort()).toEqual(['area', 'at', 'created', 'id', 'note', 'resolved']);
    expect(annotation).toMatchObject({
      id: 'a1',
      at: { x: 0.5, y: 0.5 },
      area: 'hierarchy',
      note: 'Primary CTA reads as secondary',
      resolved: false,
      created: FIXED_NOW,
    });
  });

  it('defaults the area and trims the note', async () => {
    renderAffordance();
    submitNote('  too light  ');

    await waitFor(() => expect(mockedWriteFeedback).toHaveBeenCalledTimes(1));
    const json = writtenJson();
    expect(json.annotations[0].area).toBe('hierarchy');
    expect(json.annotations[0].note).toBe('too light');
  });

  it('resolves the written path under design/feedback/ for either asset form', async () => {
    renderAffordance({ path: 'screens/login.html' });
    submitNote('centre it');

    await waitFor(() => expect(mockedWriteFeedback).toHaveBeenCalledTimes(1));
    expect(mockedWriteFeedback.mock.calls[0][1]).toBe('design/screens/login.html');
    await waitFor(() =>
      expect(screen.getByTestId('design-feedback-written').textContent).toBe('Wrote design/feedback/login.json'),
    );
  });

  it('surfaces a write failure without throwing', async () => {
    mockedWriteFeedback.mockRejectedValueOnce(new Error('nope'));
    renderAffordance();
    submitNote('will fail');

    await waitFor(() =>
      expect(screen.getByTestId('design-feedback-error').textContent).toBe(
        'Could not write design/feedback/login.json.',
      ),
    );
  });

  it('clears a previous failure once the next submit is attempted', async () => {
    mockedWriteFeedback.mockRejectedValueOnce(new Error('nope'));
    renderAffordance();
    submitNote('will fail');
    await screen.findByTestId('design-feedback-error');

    submitNote('second try');
    await waitFor(() => expect(screen.queryByTestId('design-feedback-error')).toBeNull());
    expect(screen.getByTestId('design-feedback-written').textContent).toBe('Wrote design/feedback/login.json');
  });

  it('hands the write to an onWriteFeedback override instead of designApi', async () => {
    const onWriteFeedback = vi.fn().mockResolvedValue(undefined);
    renderAffordance({ onWriteFeedback });
    submitNote('override', 'contrast');

    await waitFor(() => expect(onWriteFeedback).toHaveBeenCalledTimes(1));
    expect(mockedWriteFeedback).not.toHaveBeenCalled();
    expect(onWriteFeedback.mock.calls[0][1].annotations[0]).toMatchObject({ area: 'contrast', note: 'override' });
  });
});

describe('detail pane integration', () => {
  it('mounts the affordance for the pane selection', () => {
    render(
      <SproutAdapterProvider>
        <DesignDetailPane path="design/wireframes/login.svg" now={() => FIXED_NOW} />
      </SproutAdapterProvider>,
    );
    expect(screen.getByTestId('design-feedback-affordance').getAttribute('data-target')).toBe(
      'design/wireframes/login.svg',
    );
  });

  it('mounts no affordance before an asset is selected', () => {
    render(
      <SproutAdapterProvider>
        <DesignDetailPane path={null} />
      </SproutAdapterProvider>,
    );
    expect(screen.queryByTestId('design-feedback-affordance')).toBeNull();
    expect(screen.getByText('Select an asset to inspect it.')).toBeTruthy();
  });
});
