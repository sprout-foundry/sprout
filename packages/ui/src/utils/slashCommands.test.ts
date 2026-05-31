import { getMatchingSlashCommands, SLASH_COMMANDS } from './slashCommands';

describe('slashCommands', () => {
  describe('SLASH_COMMANDS registry', () => {
    it('contains all expected slash commands', () => {
      const names = SLASH_COMMANDS.map(c => c.name);
      expect(names).toContain('clear');
      expect(names).toContain('commit');
      expect(names).toContain('help');
      expect(names).toContain('model');
      expect(names).toContain('provider');
      expect(names).toContain('persona');
      expect(names).toContain('exit');
    });

    it('marks aliases with isAlias and aliasOf', () => {
      const aliases = SLASH_COMMANDS.filter(c => c.isAlias);
      expect(aliases.length).toBeGreaterThan(0);
      aliases.forEach(alias => {
        expect(alias.aliasOf).toBeDefined();
        expect(typeof alias.aliasOf).toBe('string');
      });
    });

    it('has unique command names', () => {
      const names = SLASH_COMMANDS.map(c => c.name);
      const unique = new Set(names);
      expect(unique.size).toBe(names.length);
    });
  });

  describe('getMatchingSlashCommands', () => {
    it('returns all commands for empty prefix', () => {
      const matches = getMatchingSlashCommands('');
      expect(matches.length).toBe(SLASH_COMMANDS.length);
    });

    it('matches by prefix (case insensitive)', () => {
      const matches = getMatchingSlashCommands('hel');
      expect(matches.map(c => c.name)).toContain('help');
      expect(matches.map(c => c.name)).not.toContain('clear');
    });

    it('matches uppercase prefix against lowercase commands', () => {
      const matches = getMatchingSlashCommands('HEL');
      expect(matches.map(c => c.name)).toContain('help');
    });

    it('returns empty array for non-matching prefix', () => {
      const matches = getMatchingSlashCommands('zzzznonexistent');
      expect(matches).toEqual([]);
    });

    it('sorts real commands before aliases', () => {
      const matches = getMatchingSlashCommands('h');
      // 'help' should come before 'h' (alias)
      const helpIndex = matches.findIndex(c => c.name === 'help');
      const hIndex = matches.findIndex(c => c.name === 'h');
      expect(helpIndex).toBeLessThan(hIndex);
    });

    it('sorts alphabetically within same category', () => {
      const matches = getMatchingSlashCommands('s');
      // subagent commands should be alphabetically sorted
      const subagentCmds = matches.filter(c => c.name.startsWith('subagent') && !c.isAlias);
      for (let i = 1; i < subagentCmds.length; i++) {
        expect(subagentCmds[i - 1].name.localeCompare(subagentCmds[i].name)).toBeLessThanOrEqual(0);
      }
    });

    it('matches single character aliases', () => {
      const matches = getMatchingSlashCommands('?');
      expect(matches.length).toBe(1);
      expect(matches[0].name).toBe('?');
      expect(matches[0].isAlias).toBe(true);
      expect(matches[0].aliasOf).toBe('help');
    });

    it('matches "review" and "review deep" separately', () => {
      const matches = getMatchingSlashCommands('review');
      expect(matches.map(c => c.name)).toContain('review');
      expect(matches.map(c => c.name)).toContain('review deep');
    });

    it('trims whitespace from prefix', () => {
      const matches1 = getMatchingSlashCommands('  help  ');
      const matches2 = getMatchingSlashCommands('help');
      expect(matches1.map(c => c.name)).toEqual(matches2.map(c => c.name));
    });
  });
});
