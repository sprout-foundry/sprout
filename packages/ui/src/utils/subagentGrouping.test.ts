import { groupSubagentRuns } from './subagentGrouping';
import type { SubagentActivity, SubagentRun } from '../types/chat';

describe('groupSubagentRuns', () => {
  describe('empty arrays', () => {
    it('returns empty array for empty input', () => {
      const result = groupSubagentRuns([]);
      expect(result).toHaveLength(0);
    });
  });

  describe('single subagent run', () => {
    it('groups activities by toolCallId', () => {
      const activities: SubagentActivity[] = [
        {
          id: '1',
          toolCallId: 'call-123',
          toolName: 'subagent',
          phase: 'spawn',
          message: 'Starting',
          timestamp: new Date('2024-01-01T10:00:00Z'),
        },
        {
          id: '2',
          toolCallId: 'call-123',
          toolName: 'subagent',
          phase: 'output',
          message: 'Working',
          timestamp: new Date('2024-01-01T10:00:01Z'),
        },
        {
          id: '3',
          toolCallId: 'call-123',
          toolName: 'subagent',
          phase: 'complete',
          message: 'Done',
          timestamp: new Date('2024-01-01T10:00:02Z'),
        },
      ];

      const result = groupSubagentRuns(activities);
      expect(result).toHaveLength(1);
      expect(result[0].toolCallId).toBe('call-123');
      expect(result[0].activities).toHaveLength(3);
    });

    it('tracks spawn and complete activities', () => {
      const activities: SubagentActivity[] = [
        {
          id: '1',
          toolCallId: 'call-123',
          toolName: 'subagent',
          phase: 'spawn',
          message: 'Start',
          timestamp: new Date(),
        },
        {
          id: '2',
          toolCallId: 'call-123',
          toolName: 'subagent',
          phase: 'complete',
          message: 'Complete',
          timestamp: new Date(),
        },
      ];

      const result = groupSubagentRuns(activities);
      expect(result[0].spawnActivity).not.toBeNull();
      expect(result[0].completeActivity).not.toBeNull();
    });

    it('marks run as complete when complete phase present', () => {
      const activities: SubagentActivity[] = [
        {
          id: '1',
          toolCallId: 'call-123',
          toolName: 'subagent',
          phase: 'spawn',
          message: 'Start',
          timestamp: new Date(),
        },
        {
          id: '2',
          toolCallId: 'call-123',
          toolName: 'subagent',
          phase: 'complete',
          message: 'Done',
          timestamp: new Date(),
        },
      ];

      const result = groupSubagentRuns(activities);
      expect(result[0].isComplete).toBe(true);
      expect(result[0].completionMessage).toBe('Done');
    });

    it('does not mark run as complete without complete phase', () => {
      const activities: SubagentActivity[] = [
        {
          id: '1',
          toolCallId: 'call-123',
          toolName: 'subagent',
          phase: 'spawn',
          message: 'Start',
          timestamp: new Date(),
        },
      ];

      const result = groupSubagentRuns(activities);
      expect(result[0].isComplete).toBe(false);
      expect(result[0].completionMessage).toBe('');
    });
  });

  describe('multiple subagent runs', () => {
    it('groups activities from different toolCallIds separately', () => {
      const activities: SubagentActivity[] = [
        {
          id: '1',
          toolCallId: 'call-1',
          toolName: 'subagent',
          phase: 'spawn',
          message: 'Spawn 1',
          timestamp: new Date(),
        },
        {
          id: '2',
          toolCallId: 'call-2',
          toolName: 'subagent',
          phase: 'spawn',
          message: 'Spawn 2',
          timestamp: new Date(),
        },
        {
          id: '3',
          toolCallId: 'call-1',
          toolName: 'subagent',
          phase: 'complete',
          message: 'Complete 1',
          timestamp: new Date(),
        },
      ];

      const result = groupSubagentRuns(activities);
      expect(result).toHaveLength(2);
      expect(result.find(r => r.toolCallId === 'call-1')).toBeDefined();
      expect(result.find(r => r.toolCallId === 'call-2')).toBeDefined();
    });

    it('handles interleaved activities from multiple subagents', () => {
      const activities: SubagentActivity[] = [
        {
          id: '1',
          toolCallId: 'call-1',
          toolName: 'subagent',
          phase: 'spawn',
          message: 'Spawn 1',
          timestamp: new Date(),
        },
        {
          id: '2',
          toolCallId: 'call-2',
          toolName: 'subagent',
          phase: 'spawn',
          message: 'Spawn 2',
          timestamp: new Date(),
        },
        {
          id: '3',
          toolCallId: 'call-1',
          toolName: 'subagent',
          phase: 'output',
          message: 'Output 1',
          timestamp: new Date(),
        },
        {
          id: '4',
          toolCallId: 'call-2',
          toolName: 'subagent',
          phase: 'output',
          message: 'Output 2',
          timestamp: new Date(),
        },
      ];

      const result = groupSubagentRuns(activities);
      expect(result).toHaveLength(2);
      expect(result[0].activities).toHaveLength(2);
      expect(result[1].activities).toHaveLength(2);
    });
  });

  describe('output lines', () => {
    it('splits multi-line output messages into individual lines', () => {
      const activities: SubagentActivity[] = [
        {
          id: '1',
          toolCallId: 'call-123',
          toolName: 'subagent',
          phase: 'output',
          message: 'Line 1\nLine 2\nLine 3',
          timestamp: new Date(),
        },
      ];

      const result = groupSubagentRuns(activities);
      expect(result[0].outputLines).toHaveLength(3);
      expect(result[0].outputLines[0].text).toBe('Line 1');
      expect(result[0].outputLines[1].text).toBe('Line 2');
      expect(result[0].outputLines[2].text).toBe('Line 3');
    });

    it('filters out empty lines from output', () => {
      const activities: SubagentActivity[] = [
        {
          id: '1',
          toolCallId: 'call-123',
          toolName: 'subagent',
          phase: 'output',
          message: 'Line 1\n\nLine 3',
          timestamp: new Date(),
        },
      ];

      const result = groupSubagentRuns(activities);
      expect(result[0].outputLines).toHaveLength(2);
      expect(result[0].outputLines[0].text).toBe('Line 1');
      expect(result[0].outputLines[1].text).toBe('Line 3');
    });

    it('preserves taskId in output lines', () => {
      const activities: SubagentActivity[] = [
        {
          id: '1',
          toolCallId: 'call-123',
          toolName: 'subagent',
          phase: 'output',
          message: 'Output',
          timestamp: new Date(),
          taskId: 'task-456',
        },
      ];

      const result = groupSubagentRuns(activities);
      expect(result[0].outputLines[0].taskId).toBe('task-456');
    });

    it('generates unique IDs for output lines', () => {
      const activities: SubagentActivity[] = [
        {
          id: '1',
          toolCallId: 'call-123',
          toolName: 'subagent',
          phase: 'output',
          message: 'Line 1\nLine 2',
          timestamp: new Date(),
        },
      ];

      const result = groupSubagentRuns(activities);
      expect(result[0].outputLines[0].id).not.toBe(result[0].outputLines[1].id);
    });
  });

  describe('step phase handling', () => {
    it('includes step phase activities in output lines', () => {
      const activities: SubagentActivity[] = [
        {
          id: '1',
          toolCallId: 'call-123',
          toolName: 'subagent',
          phase: 'step',
          message: 'Step output',
          timestamp: new Date(),
        },
      ];

      const result = groupSubagentRuns(activities);
      expect(result[0].outputLines).toHaveLength(1);
      expect(result[0].outputLines[0].text).toBe('Step output');
    });
  });

  describe('persona and parallel tracking', () => {
    it('tracks persona from spawn activity', () => {
      const activities: SubagentActivity[] = [
        {
          id: '1',
          toolCallId: 'call-123',
          toolName: 'subagent',
          phase: 'spawn',
          message: 'Spawn',
          timestamp: new Date(),
          persona: 'coder',
        },
      ];

      const result = groupSubagentRuns(activities);
      expect(result[0].persona).toBe('coder');
    });

    it('updates persona from later activities if spawn missing', () => {
      const activities: SubagentActivity[] = [
        {
          id: '1',
          toolCallId: 'call-123',
          toolName: 'subagent',
          phase: 'output',
          message: 'Output',
          timestamp: new Date(),
          persona: 'editor',
        },
      ];

      const result = groupSubagentRuns(activities);
      expect(result[0].persona).toBe('editor');
    });

    it('tracks parallel execution flag', () => {
      const activities: SubagentActivity[] = [
        {
          id: '1',
          toolCallId: 'call-123',
          toolName: 'subagent',
          phase: 'spawn',
          message: 'Spawn',
          timestamp: new Date(),
          isParallel: true,
        },
      ];

      const result = groupSubagentRuns(activities);
      expect(result[0].isParallel).toBe(true);
    });
  });

  describe('fallback to id when toolCallId missing', () => {
    it('uses activity id as grouping key when toolCallId is missing', () => {
      const activities: SubagentActivity[] = [
        {
          id: 'activity-1',
          toolCallId: '',
          toolName: 'subagent',
          phase: 'spawn',
          message: 'Spawn',
          timestamp: new Date(),
        },
      ];

      const result = groupSubagentRuns(activities);
      expect(result).toHaveLength(1);
      expect(result[0].activities).toHaveLength(1);
    });
  });

  describe('completion timestamp', () => {
    it('sets completion timestamp from complete activity', () => {
      const timestamp = new Date('2024-01-01T10:00:00Z');
      const activities: SubagentActivity[] = [
        {
          id: '1',
          toolCallId: 'call-123',
          toolName: 'subagent',
          phase: 'complete',
          message: 'Done',
          timestamp,
        },
      ];

      const result = groupSubagentRuns(activities);
      expect(result[0].completionTimestamp).toEqual(timestamp);
    });

    it('has null completion timestamp when not complete', () => {
      const activities: SubagentActivity[] = [
        {
          id: '1',
          toolCallId: 'call-123',
          toolName: 'subagent',
          phase: 'spawn',
          message: 'Spawn',
          timestamp: new Date(),
        },
      ];

      const result = groupSubagentRuns(activities);
      expect(result[0].completionTimestamp).toBeNull();
    });
  });

  describe('complex scenarios', () => {
    it('handles run with multiple output and step phases', () => {
      const activities: SubagentActivity[] = [
        {
          id: '1',
          toolCallId: 'call-123',
          toolName: 'subagent',
          phase: 'spawn',
          message: 'Spawn',
          timestamp: new Date(),
          persona: 'coder',
        },
        {
          id: '2',
          toolCallId: 'call-123',
          toolName: 'subagent',
          phase: 'output',
          message: 'Line 1\nLine 2',
          timestamp: new Date(),
        },
        {
          id: '3',
          toolCallId: 'call-123',
          toolName: 'subagent',
          phase: 'step',
          message: 'Step 1',
          timestamp: new Date(),
        },
        {
          id: '4',
          toolCallId: 'call-123',
          toolName: 'subagent',
          phase: 'complete',
          message: 'Complete',
          timestamp: new Date(),
        },
      ];

      const result = groupSubagentRuns(activities);
      expect(result).toHaveLength(1);
      expect(result[0].activities).toHaveLength(4);
      expect(result[0].outputLines).toHaveLength(3); // 2 from output + 1 from step
      expect(result[0].persona).toBe('coder');
      expect(result[0].isComplete).toBe(true);
    });

    it('handles parallel and sequential runs mixed', () => {
      const activities: SubagentActivity[] = [
        {
          id: '1',
          toolCallId: 'call-1',
          toolName: 'subagent',
          phase: 'spawn',
          message: 'Spawn 1',
          timestamp: new Date(),
          isParallel: true,
        },
        {
          id: '2',
          toolCallId: 'call-2',
          toolName: 'subagent',
          phase: 'spawn',
          message: 'Spawn 2',
          timestamp: new Date(),
          isParallel: false,
        },
      ];

      const result = groupSubagentRuns(activities);
      expect(result).toHaveLength(2);
      expect(result.find(r => r.toolCallId === 'call-1')?.isParallel).toBe(true);
      expect(result.find(r => r.toolCallId === 'call-2')?.isParallel).toBe(false);
    });
  });

  describe('depth propagation', () => {
    it('defaults depth to 0 when activities have no depth', () => {
      const activities: SubagentActivity[] = [
        {
          id: '1',
          toolCallId: 'call-123',
          toolName: 'subagent',
          phase: 'spawn',
          message: 'Spawn',
          timestamp: new Date(),
        },
      ];

      const result = groupSubagentRuns(activities);
      expect(result[0].depth).toBe(0);
    });

    it('uses depth from first activity that has it', () => {
      const activities: SubagentActivity[] = [
        {
          id: '1',
          toolCallId: 'call-123',
          toolName: 'subagent',
          phase: 'spawn',
          message: 'Spawn',
          timestamp: new Date(),
          depth: 2,
        },
        {
          id: '2',
          toolCallId: 'call-123',
          toolName: 'subagent',
          phase: 'output',
          message: 'Output',
          timestamp: new Date(),
          depth: 2,
        },
      ];

      const result = groupSubagentRuns(activities);
      expect(result[0].depth).toBe(2);
    });

    it('handles mixed depth values across activities in same run (uses first one)', () => {
      const activities: SubagentActivity[] = [
        {
          id: '1',
          toolCallId: 'call-123',
          toolName: 'subagent',
          phase: 'spawn',
          message: 'Spawn',
          timestamp: new Date(),
          depth: 1,
        },
        {
          id: '2',
          toolCallId: 'call-123',
          toolName: 'subagent',
          phase: 'output',
          message: 'Output',
          timestamp: new Date(),
          depth: 3,
        },
        {
          id: '3',
          toolCallId: 'call-123',
          toolName: 'subagent',
          phase: 'complete',
          message: 'Complete',
          timestamp: new Date(),
          depth: 0,
        },
      ];

      const result = groupSubagentRuns(activities);
      // The run's depth is determined by the first activity that creates it
      expect(result[0].depth).toBe(1);
    });

    it('defaults to 0 when first activity has undefined depth but later ones have depth', () => {
      const activities: SubagentActivity[] = [
        {
          id: '1',
          toolCallId: 'call-123',
          toolName: 'subagent',
          phase: 'spawn',
          message: 'Spawn',
          timestamp: new Date(),
          // no depth
        },
        {
          id: '2',
          toolCallId: 'call-123',
          toolName: 'subagent',
          phase: 'output',
          message: 'Output',
          timestamp: new Date(),
          depth: 2,
        },
      ];

      const result = groupSubagentRuns(activities);
      // First activity had no depth, so the run defaults to 0
      expect(result[0].depth).toBe(0);
    });

    it('preserves distinct depth values across multiple runs', () => {
      const activities: SubagentActivity[] = [
        {
          id: '1',
          toolCallId: 'call-1',
          toolName: 'subagent',
          phase: 'spawn',
          message: 'Spawn 1',
          timestamp: new Date(),
          depth: 1,
        },
        {
          id: '2',
          toolCallId: 'call-2',
          toolName: 'subagent',
          phase: 'spawn',
          message: 'Spawn 2',
          timestamp: new Date(),
          depth: 2,
        },
        {
          id: '3',
          toolCallId: 'call-3',
          toolName: 'subagent',
          phase: 'spawn',
          message: 'Spawn 3',
          timestamp: new Date(),
        },
      ];

      const result = groupSubagentRuns(activities);
      expect(result.find(r => r.toolCallId === 'call-1')?.depth).toBe(1);
      expect(result.find(r => r.toolCallId === 'call-2')?.depth).toBe(2);
      expect(result.find(r => r.toolCallId === 'call-3')?.depth).toBe(0);
    });
  });
});
