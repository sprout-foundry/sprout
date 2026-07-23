/**
 * parseSecurityAnalysis tests — SP-124 Phase 2.
 *
 * The parser is the single point of safety between the WS event (Go JSON
 * string) and the React state (typed camelCase object). It must NEVER
 * throw, must handle malformed JSON, missing fields, and unknown values
 * gracefully (returning undefined or empty strings), per the SP-124
 * "analyzer is non-blocking" contract.
 */
import { describe, it, expect } from 'vitest';
import { parseSecurityAnalysis, parseSecurityAnalysisString } from './parseSecurityAnalysis';

describe('parseSecurityAnalysisString', () => {
  it('parses valid JSON with all four fields', () => {
    const sa = parseSecurityAnalysisString(
      JSON.stringify({
        summary: 'Deletes build artifacts',
        modifies: './build/ only',
        risk_assessment: 'low',
        recommendation: 'approve',
      }),
    );
    expect(sa).toEqual({
      summary: 'Deletes build artifacts',
      modifies: './build/ only',
      riskAssessment: 'low',
      recommendation: 'approve',
    });
  });

  it('returns undefined for malformed JSON', () => {
    expect(parseSecurityAnalysisString('{ not valid json')).toBeUndefined();
    expect(parseSecurityAnalysisString('undefined')).toBeUndefined();
    expect(parseSecurityAnalysisString('null')).toBeUndefined();
  });

  it('returns undefined for empty / non-string inputs', () => {
    expect(parseSecurityAnalysisString('')).toBeUndefined();
    expect(parseSecurityAnalysisString('   ')).toBeUndefined();
    expect(parseSecurityAnalysisString(undefined)).toBeUndefined();
    expect(parseSecurityAnalysisString(null)).toBeUndefined();
    expect(parseSecurityAnalysisString(42)).toBeUndefined();
    expect(parseSecurityAnalysisString({ summary: 'x' })).toBeUndefined();
    expect(parseSecurityAnalysisString(['x'])).toBeUndefined();
  });

  it('returns undefined for `{}` (no meaningful fields)', () => {
    expect(parseSecurityAnalysisString('{}')).toBeUndefined();
  });

  it('returns undefined when all four fields are empty/missing', () => {
    expect(parseSecurityAnalysisString(JSON.stringify({}))).toBeUndefined();
    expect(
      parseSecurityAnalysisString(
        JSON.stringify({ summary: '', modifies: '', risk_assessment: '', recommendation: '' }),
      ),
    ).toBeUndefined();
  });

  it('parses JSON with extra unknown fields (ignores them, returns typed shape)', () => {
    const sa = parseSecurityAnalysisString(
      JSON.stringify({
        summary: 'x',
        modifies: 'y',
        risk_assessment: 'moderate',
        recommendation: 'review',
        extra_garbage: 'ignore me',
        nested: { foo: 'bar' },
      }),
    );
    expect(sa).toEqual({
      summary: 'x',
      modifies: 'y',
      riskAssessment: 'moderate',
      recommendation: 'review',
    });
  });

  it('returns object with one valid field when other fields are non-string', () => {
    // The all-empty guard at the bottom of the parser only triggers when
    // EVERY field is empty/missing. With one valid field present, the
    // parsed object is returned with empty-string fall-throughs for the
    // rest — so callers can still rely on summary, even partial.
    const sa = parseSecurityAnalysisString(
      JSON.stringify({ summary: 'partial', modifies: null, risk_assessment: 42, recommendation: undefined }),
    );
    expect(sa).toEqual({
      summary: 'partial',
      modifies: '',
      riskAssessment: '',
      recommendation: '',
    });
  });

  it('returns undefined for JSON non-object root (array, number, string)', () => {
    expect(parseSecurityAnalysisString('[1,2,3]')).toBeUndefined();
    expect(parseSecurityAnalysisString('"hello"')).toBeUndefined();
    expect(parseSecurityAnalysisString('42')).toBeUndefined();
    expect(parseSecurityAnalysisString('true')).toBeUndefined();
  });
});

describe('parseSecurityAnalysis', () => {
  it('extracts security_analysis field from event payload and parses it', () => {
    const eventData = {
      request_id: 'sec_42',
      command: 'rm -rf build/',
      security_analysis: JSON.stringify({
        summary: 'removes build artifacts',
        modifies: 'build/ directory',
        risk_assessment: 'low',
        recommendation: 'approve',
      }),
    };
    expect(parseSecurityAnalysis(eventData)).toEqual({
      summary: 'removes build artifacts',
      modifies: 'build/ directory',
      riskAssessment: 'low',
      recommendation: 'approve',
    });
  });

  it('returns undefined when security_analysis field is absent', () => {
    expect(parseSecurityAnalysis({ request_id: 'sec_1', command: 'ls' })).toBeUndefined();
  });

  it('returns undefined when security_analysis is malformed', () => {
    expect(parseSecurityAnalysis({ security_analysis: '{ broken json' })).toBeUndefined();
  });

  it('returns undefined for null / non-object inputs', () => {
    expect(parseSecurityAnalysis(null)).toBeUndefined();
    expect(parseSecurityAnalysis(undefined)).toBeUndefined();
    expect(parseSecurityAnalysis('string')).toBeUndefined();
    expect(parseSecurityAnalysis(42)).toBeUndefined();
  });
});
