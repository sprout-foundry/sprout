/**
 * SP-140-4 item 4.8 — the detail pane's resolution flow.
 *
 * Covers the §4d contract:
 * - Marking an annotation `resolved` writes the updated feedback file through
 *   the write seam (asserted on the document handed to the seam), and toggling
 *   back to unresolved writes it again with the flag cleared.
 * - Setting the top-level `resolution` note and saving writes the note (and the
 *   derived `status`) back.
 * - The feedback file round-trips the §4d schema: the document written is the
 *   one read back, with every field present.
 *
 * `designApi` is mocked (the `readFeedback`/`writeFeedback` calls are the
 * assertion targets) while the real pure `feedbackWrite` model stays in play,
 * per the DesignFeedbackAffordance.test convention. Assertions use the suite's
 * plain-expect style rather than `@testing-library/jest-dom` matchers.
 */

import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { SproutAdapterProvider } from '../../contexts/SproutAdapterContext';
import { readFeedback, parseFeedbackJson, writeFeedback } from '../../services/api/designApi';
import type { DesignFeedbackFile } from '../../services/api/types';
import DesignDetailPane from './DesignDetailPane';
import DesignFeedbackResolution from './DesignFeedbackResolution';

vi.mock('../../services/api/designApi', async () => {
  const actual = await vi.importActual<Record<string, unknown>>('../../services/api/designApi');
  return { ...actual, readFeedback: vi.fn(), writeFeedback: vi.fn() };
});

const mockedReadFeedback = vi.mocked(readFeedback);
const mockedWriteFeedback = vi.mocked(writeFeedback);

const TARGET = 'design/screens/login.html';
const FILE = 'design/feedback/login.json';

/** The §4d document a written file contains, as the pane would read it back. */
function feedbackFile(overrides: Partial<DesignFeedbackFile> = {}): DesignFeedbackFile {
  return {
    target: TARGET,
    status: 'changes-requested',
    resolution: '',
    annotations: [
      {
        id: 'a1',
        at: { x: 0.42, y: 0.18 },
        area: 'hierarchy',
        note: 'Primary CTA reads as secondary',
        resolved: false,
        created: '2026-09-15T10:36:47Z',
      },
      {
        id: 'a2',
        at: { x: 0.1, y: 0.9 },
        area: 'contrast',
        note: 'Caption fails contrast',
        resolved: false,
        created: '2026-09-15T10:40:00Z',
      },
    ],
    ...overrides,
  };
}

function renderResolution(props: Partial<React.ComponentProps<typeof DesignFeedbackResolution>> = {}) {
  return render(
    <SproutAdapterProvider>
      <DesignFeedbackResolution path={TARGET} {...props} />
    </SproutAdapterProvider>,
  );
}

/** The `DesignFeedbackFile` handed to the mocked write on its last call. */
function writtenJson(): DesignFeedbackFile {
  return mockedWriteFeedback.mock.calls.at(-1)![2];
}

beforeEach(() => {
  mockedReadFeedback.mockReset();
  mockedWriteFeedback.mockReset();
  mockedReadFeedback.mockResolvedValue(feedbackFile());
  mockedWriteFeedback.mockResolvedValue({
    path: FILE,
    content: '{}',
    response: { ok: true, status: 200 } as Response,
  });
});

describe('feedback resolution rendering', () => {
  it('reads the §4d file for the selected asset and lists its annotations', async () => {
    renderResolution();
    expect(await screen.findByTestId('design-feedback-annotations')).toBeTruthy();

    expect(mockedReadFeedback).toHaveBeenCalledTimes(1);
    expect(mockedReadFeedback.mock.calls[0][1]).toBe(TARGET);

    expect(screen.getByTestId('design-feedback-annotation-a1').getAttribute('data-resolved')).toBe('false');
    expect(screen.getByTestId('design-feedback-area-a1').textContent).toBe('hierarchy');
    expect(screen.getByTestId('design-feedback-annotation-a2').getAttribute('data-resolved')).toBe('false');
    expect(screen.getByTestId('design-feedback-count').textContent).toBe('0 of 2 resolved');
    expect(screen.getByTestId('design-feedback-status').textContent).toBe('changes-requested');
  });

  it('renders nothing without a selected asset', () => {
    render(
      <SproutAdapterProvider>
        <DesignFeedbackResolution path={null} />
      </SproutAdapterProvider>,
    );
    expect(screen.queryByTestId('design-feedback-resolution')).toBeNull();
    expect(mockedReadFeedback).not.toHaveBeenCalled();
  });

  it('shows the empty state for a target with no annotations (still allowing a note)', async () => {
    mockedReadFeedback.mockResolvedValue(feedbackFile({ annotations: [] }));
    renderResolution();
    expect(await screen.findByTestId('design-feedback-empty')).toBeTruthy();
    expect(screen.queryByTestId('design-feedback-annotations')).toBeNull();
    expect((screen.getByTestId('design-feedback-resolution-save') as HTMLButtonElement).disabled).toBe(false);
  });

  it('surfaces a read failure without throwing', async () => {
    mockedReadFeedback.mockRejectedValue(new Error('nope'));
    renderResolution();
    await waitFor(() =>
      expect(screen.getByTestId('design-feedback-resolution-error').textContent).toBe(`Could not read ${FILE}.`),
    );
    expect(screen.queryByTestId('design-feedback-annotations')).toBeNull();
  });

  it('shows the resolved marker once the read document is fully resolved', async () => {
    mockedReadFeedback.mockResolvedValue(
      feedbackFile({
        status: 'resolved',
        resolution: 'done',
        annotations: feedbackFile().annotations.map((a) => ({ ...a, resolved: true })),
      }),
    );
    renderResolution();
    expect(await screen.findByTestId('design-feedback-status')).toBeDefined();
    expect(screen.getByTestId('design-feedback-status').getAttribute('data-status')).toBe('resolved');
    expect(screen.getByTestId('design-feedback-count').textContent).toBe('2 of 2 resolved');
  });
});

describe('marking an annotation resolved', () => {
  it('writes the updated §4d file with the toggled flag', async () => {
    renderResolution();
    await screen.findByTestId('design-feedback-annotations');

    fireEvent.click(screen.getByTestId('design-feedback-toggle-a1'));

    await waitFor(() => expect(mockedWriteFeedback).toHaveBeenCalledTimes(1));
    const [transport, target, json] = mockedWriteFeedback.mock.calls[0];
    expect(transport).toBeInstanceOf(Function);
    // The write seam is keyed on the file stem (writeFeedback appends
    // `feedback/<stem>.json`); handing it the asset path would nest the path.
    expect(target).toBe('login');
    expect(json.target).toBe(TARGET);
    expect(json.annotations.map((a) => [a.id, a.resolved])).toEqual([
      ['a1', true],
      ['a2', false],
    ]);
    // The untouched annotation is preserved verbatim.
    expect(json.annotations[1]).toEqual(feedbackFile().annotations[1]);
    // Every §4d field is present on the written document.
    expect(Object.keys(json).sort()).toEqual(['annotations', 'resolution', 'status', 'target']);
  });

  it('reflects the toggle in the pane and derives the status once all are resolved', async () => {
    renderResolution();
    await screen.findByTestId('design-feedback-annotations');

    fireEvent.click(screen.getByTestId('design-feedback-toggle-a1'));
    await waitFor(() => expect(screen.getByTestId('design-feedback-count').textContent).toBe('1 of 2 resolved'));
    expect(screen.getByTestId('design-feedback-annotation-a1').getAttribute('data-resolved')).toBe('true');
    expect(screen.getByTestId('design-feedback-toggle-a1').textContent).toBe('Mark unresolved');
    expect(writtenJson().status).toBe('changes-requested');

    fireEvent.click(screen.getByTestId('design-feedback-toggle-a2'));
    await waitFor(() => expect(mockedWriteFeedback).toHaveBeenCalledTimes(2));
    expect(writtenJson().status).toBe('resolved');
    expect(screen.getByTestId('design-feedback-status').getAttribute('data-status')).toBe('resolved');
  });

  it('toggles back to unresolved and reopens the file', async () => {
    renderResolution();
    await screen.findByTestId('design-feedback-annotations');

    fireEvent.click(screen.getByTestId('design-feedback-toggle-a1'));
    await screen.findByTestId('design-feedback-count');
    await waitFor(() => expect(writtenJson().annotations[0].resolved).toBe(true));

    fireEvent.click(screen.getByTestId('design-feedback-toggle-a1'));
    await waitFor(() => expect(mockedWriteFeedback).toHaveBeenCalledTimes(2));
    expect(writtenJson().annotations[0].resolved).toBe(false);
    expect(writtenJson().status).toBe('changes-requested');
    expect(screen.getByTestId('design-feedback-annotation-a1').getAttribute('data-resolved')).toBe('false');
  });

  it('reports the written path and surfaces a write failure', async () => {
    mockedWriteFeedback.mockRejectedValueOnce(new Error('nope'));
    renderResolution();
    await screen.findByTestId('design-feedback-annotations');

    fireEvent.click(screen.getByTestId('design-feedback-toggle-a1'));
    await waitFor(() =>
      expect(screen.getByTestId('design-feedback-resolution-error').textContent).toBe(`Could not write ${FILE}.`),
    );

    fireEvent.click(screen.getByTestId('design-feedback-toggle-a1'));
    await waitFor(() =>
      expect(screen.getByTestId('design-feedback-resolution-written').textContent).toBe(`Wrote ${FILE}`),
    );
    expect(screen.queryByTestId('design-feedback-resolution-error')).toBeNull();
  });
});

describe('closing the loop with a resolution note', () => {
  it('writes the note and the derived status', async () => {
    renderResolution();
    await screen.findByTestId('design-feedback-annotations');

    fireEvent.change(screen.getByTestId('design-feedback-resolution-note'), {
      target: { value: '  swapped the CTA emphasis with the link below  ' },
    });
    fireEvent.click(screen.getByTestId('design-feedback-resolution-save'));

    await waitFor(() => expect(mockedWriteFeedback).toHaveBeenCalledTimes(1));
    const json = writtenJson();
    expect(json.resolution).toBe('swapped the CTA emphasis with the link below');
    // Annotations are still open, so the file stays pending.
    expect(json.status).toBe('changes-requested');
    expect(json.annotations.every((a) => a.resolved === false)).toBe(true);
  });

  it('flips the status to resolved when the annotations are all resolved', async () => {
    renderResolution();
    await screen.findByTestId('design-feedback-annotations');

    fireEvent.click(screen.getByTestId('design-feedback-toggle-a1'));
    await waitFor(() => expect(mockedWriteFeedback).toHaveBeenCalledTimes(1));
    fireEvent.click(screen.getByTestId('design-feedback-toggle-a2'));
    await waitFor(() => expect(mockedWriteFeedback).toHaveBeenCalledTimes(2));

    fireEvent.change(screen.getByTestId('design-feedback-resolution-note'), {
      target: { value: 'rewrote the header block' },
    });
    fireEvent.click(screen.getByTestId('design-feedback-resolution-save'));

    await waitFor(() => expect(mockedWriteFeedback).toHaveBeenCalledTimes(3));
    const json = writtenJson();
    expect(json.resolution).toBe('rewrote the header block');
    expect(json.status).toBe('resolved');
    expect(json.annotations.every((a) => a.resolved)).toBe(true);
  });

  it('prefills the note from the file and closes an annotation-less file', async () => {
    mockedReadFeedback.mockResolvedValue(feedbackFile({ annotations: [], resolution: 'prior note' }));
    renderResolution();
    const note = (await screen.findByTestId('design-feedback-resolution-note')) as HTMLTextAreaElement;
    expect(note.value).toBe('prior note');

    fireEvent.change(note, { target: { value: 'final note' } });
    fireEvent.click(screen.getByTestId('design-feedback-resolution-save'));

    await waitFor(() => expect(mockedWriteFeedback).toHaveBeenCalledTimes(1));
    expect(writtenJson().resolution).toBe('final note');
    expect(writtenJson().status).toBe('resolved');
  });
});

describe('round trip and integration', () => {
  it('round-trips the §4d schema: the written document parses back to itself', async () => {
    renderResolution();
    await screen.findByTestId('design-feedback-annotations');

    fireEvent.click(screen.getByTestId('design-feedback-toggle-a1'));
    await waitFor(() => expect(mockedWriteFeedback).toHaveBeenCalledTimes(1));
    fireEvent.change(screen.getByTestId('design-feedback-resolution-note'), {
      target: { value: 'swapped the CTA emphasis' },
    });
    fireEvent.click(screen.getByTestId('design-feedback-resolution-save'));
    await waitFor(() => expect(mockedWriteFeedback).toHaveBeenCalledTimes(2));

    const written = writtenJson();
    // The §4d key set is exactly right on both the document and its annotations.
    expect(Object.keys(written).sort()).toEqual(['annotations', 'resolution', 'status', 'target']);
    for (const annotation of written.annotations) {
      expect(Object.keys(annotation).sort()).toEqual(['area', 'at', 'created', 'id', 'note', 'resolved']);
    }

    // Feed the written document back through the reader: it parses to itself,
    // so a save → reload never drops or invents a field.
    const reparsed = parseFeedbackJson(JSON.stringify(written), TARGET);
    expect(reparsed).toEqual(written);
  });
  it('hands the read/write to the onReadFeedback/onWriteFeedback overrides', async () => {
    const onReadFeedback = vi.fn().mockResolvedValue(feedbackFile());
    const onWriteFeedback = vi.fn().mockResolvedValue(undefined);
    renderResolution({ onReadFeedback, onWriteFeedback });
    await screen.findByTestId('design-feedback-annotations');

    expect(mockedReadFeedback).not.toHaveBeenCalled();
    fireEvent.click(screen.getByTestId('design-feedback-toggle-a1'));

    await waitFor(() => expect(onWriteFeedback).toHaveBeenCalledTimes(1));
    expect(mockedWriteFeedback).not.toHaveBeenCalled();
    expect(onWriteFeedback.mock.calls[0][0]).toBe(TARGET);
    expect(onWriteFeedback.mock.calls[0][1].annotations[0].resolved).toBe(true);
  });

  it('is mounted by the detail pane for the selected asset', async () => {
    render(
      <SproutAdapterProvider>
        <DesignDetailPane path={TARGET} />
      </SproutAdapterProvider>,
    );
    const section = await screen.findByTestId('design-feedback-resolution');
    expect(section.getAttribute('data-target')).toBe(TARGET);
  });

  it('mounts no resolution flow before an asset is selected', () => {
    render(
      <SproutAdapterProvider>
        <DesignDetailPane path={null} />
      </SproutAdapterProvider>,
    );
    expect(screen.queryByTestId('design-feedback-resolution')).toBeNull();
  });
});
