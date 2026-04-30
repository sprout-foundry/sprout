// Mock ThemedDialog for both webui and @sprout/ui imports
const showThemedConfirm = jest.fn().mockResolvedValue(false);
const showThemedPrompt = jest.fn().mockResolvedValue(null);
module.exports = { showThemedConfirm, showThemedPrompt };
