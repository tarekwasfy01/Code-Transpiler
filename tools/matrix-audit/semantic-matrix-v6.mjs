import fs from 'node:fs/promises';
import path from 'node:path';
import {createHash} from 'node:crypto';

const outDir = path.resolve('outputs/transpiler-audit-2026-08-30');
const v5Path = path.join(outDir, 'semantic_matrix_v5.json');
const v5Bytes = await fs.readFile(v5Path);
const v5 = JSON.parse(v5Bytes);
const {languages, semantics, lexical_axes: lexicalAxes} = v5.axes;
const routes = v5.dimensions.routes;
const base = v5.matrices.design;
const source = v5.matrices.source_incidence;
const target = v5.matrices.target_incidence;
const lexical = v5.matrices.route_lexical_axis_binary;
const semantic = v5.matrices.route_semantic_binary;
const stageFailure = v5.matrices.stage_failure;
const stageKnown = v5.matrices.stage_known;

const zeros = (rows, cols) => Array.from({length: rows}, () => Array(cols).fill(0));
const kronRows = (left, right) => left.map((lrow, i) => {
  const row = Array(lrow.length * right[i].length).fill(0);
  for (let l = 0; l < lrow.length; l++) {
    if (!lrow[l]) continue;
    for (let r = 0; r < right[i].length; r++) row[l * right[i].length + r] = lrow[l] * right[i][r];
  }
  return row;
});
const sourceLexical = kronRows(source, lexical);
const targetSemantic = kronRows(target, semantic);
const design = base.map((row, i) => [...row, ...sourceLexical[i], ...targetSemantic[i]]);

function crossGram(left, right) {
  const result = zeros(left[0].length, right[0].length);
  for (let i = 0; i < left.length; i++) {
    const li = [], ri = [];
    for (let j = 0; j < left[i].length; j++) if (left[i][j]) li.push([j, left[i][j]]);
    for (let j = 0; j < right[i].length; j++) if (right[i][j]) ri.push([j, right[i][j]]);
    for (const [j, a] of li) for (const [k, b] of ri) result[j][k] += a * b;
  }
  return result;
}
function vectorProjection(matrix, vector) {
  const result = Array(matrix[0].length).fill(0);
  for (let i = 0; i < matrix.length; i++) if (vector[i]) {
    for (let j = 0; j < matrix[i].length; j++) result[j] += matrix[i][j] * vector[i];
  }
  return result;
}
const failureVector = stageFailure.map(row => row.reduce((sum, value) => sum + value, 0));
const knownVector = stageKnown.map(row => row.reduce((sum, value) => sum + value, 0));
const failureLoad = vectorProjection(design, failureVector);
const knownExposure = vectorProjection(design, knownVector);
const failureRate = failureLoad.map((value, i) => knownExposure[i] ? value / knownExposure[i] : null);
const columns = [
  ...v5.axes.design_columns,
  ...languages.flatMap(language => lexicalAxes.map(axis => `source_${language}__lex_${axis}`)),
  ...languages.flatMap(language => semantics.map(axis => `target_${language}__sem_${axis}`)),
];
const ranked = columns.map((column, i) => ({column, family: i < base[0].length ? 'base' : i < base[0].length + sourceLexical[0].length ? 'source_lexical' : 'target_semantic', failure_load: failureLoad[i], known_exposure: knownExposure[i], failure_rate: failureRate[i]}))
  .filter(row => row.known_exposure > 0)
  .sort((a, b) => b.failure_load - a.failure_load || b.failure_rate - a.failure_rate || a.column.localeCompare(b.column));
const interactionRanked = ranked.filter(row => row.family !== 'base');
const interactionRates = [...interactionRanked].filter(row => row.known_exposure >= 12)
  .sort((a, b) => b.failure_rate - a.failure_rate || b.failure_load - a.failure_load || a.column.localeCompare(b.column));

const grams = {
  base_base: v5.matrices.gram,
  base_source_lexical: crossGram(base, sourceLexical),
  base_target_semantic: crossGram(base, targetSemantic),
  source_lexical_source_lexical: crossGram(sourceLexical, sourceLexical),
  source_lexical_target_semantic: crossGram(sourceLexical, targetSemantic),
  target_semantic_target_semantic: crossGram(targetSemantic, targetSemantic),
};
const gramCells = Object.values(grams).reduce((sum, block) => sum + block.length * block[0].length, 0);
const result = {
  schema_version: 6,
  input: {
    v5_sha256: createHash('sha256').update(v5Bytes).digest('hex'),
    measurement_source_hash: v5.input.measurement_source_hash,
  },
  dimensions: {
    routes,
    base_columns: base[0].length,
    source_lexical_columns: sourceLexical[0].length,
    target_semantic_columns: targetSemantic[0].length,
    expanded_columns: design[0].length,
    expanded_cells: design.length * design[0].length,
    factorized_gram_cells: gramCells,
    unfactorized_gram_cells: design[0].length * design[0].length,
  },
  axes: {languages, lexical_axes: lexicalAxes, semantics, design_columns: columns},
  matrices: {
    source_lexical_interaction: sourceLexical,
    target_semantic_interaction: targetSemantic,
    expanded_design: design,
    factorized_gram: grams,
  },
  vectors: {route_failure_count: failureVector, route_known_count: knownVector, failure_load: failureLoad, known_exposure: knownExposure, failure_rate: failureRate},
  summary: {
    top_failure_coordinates: ranked.slice(0, 40),
    top_interaction_failure_load: interactionRanked.slice(0, 40),
    top_interaction_failure_rates: interactionRates.slice(0, 40),
    zero_exposure_columns: knownExposure.filter(value => value === 0).length,
  },
  rules: [
    'Source-language by lexical-axis interactions are row-wise Kronecker products.',
    'Target-language by semantic-axis interactions are row-wise Kronecker products.',
    'No route weights or manually assigned priorities are used.',
    'The Gram matrix is stored in exact factorized blocks; together the blocks reconstruct X transpose X.',
    'Failure load is X transpose times the route failure-count vector; known exposure is X transpose times the route known-count vector.',
  ],
};
await fs.writeFile(path.join(outDir, 'semantic_matrix_v6.json'), JSON.stringify(result));
const summary = {schema_version: result.schema_version, input: result.input, dimensions: result.dimensions, summary: result.summary, rules: result.rules};
await fs.writeFile(path.join(outDir, 'semantic_matrix_v6_summary.json'), JSON.stringify(summary, null, 2));
console.log(JSON.stringify(summary, null, 2));
