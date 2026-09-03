import fs from 'node:fs/promises';
import path from 'node:path';
import {createHash} from 'node:crypto';

const outDir = path.resolve('outputs/transpiler-audit-2026-08-30');
const v5Bytes = await fs.readFile(path.join(outDir, 'semantic_matrix_v5.json'));
const v6Bytes = await fs.readFile(path.join(outDir, 'semantic_matrix_v6.json'));
const v5 = JSON.parse(v5Bytes);
const v6 = JSON.parse(v6Bytes);
const contract = v5.matrices.fixture_contract;
const lexical = v5.matrices.fixture_lexical_axis_binary;
const projection = v5.matrices.route_fixture_projection_sparse;
const source = v5.matrices.source_incidence;
const failure = v6.vectors.route_failure_count;
const known = v6.vectors.route_known_count;
const oldDesign = v6.matrices.expanded_design;
const features = v5.axes.features;
const axes = v5.axes.lexical_axes;
const languages = v5.axes.languages;

const zeros = (rows, cols) => Array.from({length: rows}, () => Array(cols).fill(0));
const featureAxisSum = zeros(features.length, axes.length);
const featureCount = Array(features.length).fill(0);
for (let i = 0; i < contract.length; i++) {
  const feature = contract[i].findIndex(value => value !== 0);
  featureCount[feature]++;
  for (let axis = 0; axis < axes.length; axis++) featureAxisSum[feature][axis] += lexical[i][axis];
}
const featureAxisMean = featureAxisSum.map((row, feature) => row.map(value => value / featureCount[feature]));
const fixtureExpected = contract.map(row => {
  const feature = row.findIndex(value => value !== 0);
  return featureAxisMean[feature];
});
const fixtureResidual = lexical.map((row, i) => row.map((value, axis) => value - fixtureExpected[i][axis]));
const routeResidual = projection.map(index => fixtureResidual[index]);
const sourceResidual = source.map((sourceRow, i) => {
  const row = Array(languages.length * axes.length).fill(0);
  const language = sourceRow.findIndex(value => value !== 0);
  for (let axis = 0; axis < axes.length; axis++) row[language * axes.length + axis] = routeResidual[i][axis];
  return row;
});

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
function project(matrix, vector, square = false) {
  const out = Array(matrix[0].length).fill(0);
  for (let i = 0; i < matrix.length; i++) for (let j = 0; j < matrix[i].length; j++) {
    const value = square ? matrix[i][j] * matrix[i][j] : matrix[i][j];
    out[j] += value * vector[i];
  }
  return out;
}
const residualFailure = project(sourceResidual, failure);
const residualKnownEnergy = project(sourceResidual, known, true);
const residualInfluence = residualFailure.map((value, i) => residualKnownEnergy[i] ? Math.abs(value) / Math.sqrt(residualKnownEnergy[i]) : null);
const columns = languages.flatMap(language => axes.map(axis => `source_${language}__residual_${axis}`));
const ranked = columns.map((column, i) => ({column, signed_failure_projection: residualFailure[i], known_energy: residualKnownEnergy[i], standardized_influence: residualInfluence[i]}))
  .filter(row => row.known_energy > 0)
  .sort((a, b) => b.standardized_influence - a.standardized_influence || Math.abs(b.signed_failure_projection) - Math.abs(a.signed_failure_projection) || a.column.localeCompare(b.column));
const oldNewGram = crossGram(oldDesign, sourceResidual);
const newNewGram = crossGram(sourceResidual, sourceResidual);
const expandedColumns = oldDesign[0].length + sourceResidual[0].length;
const result = {
  schema_version: 7,
  input: {v5_sha256: createHash('sha256').update(v5Bytes).digest('hex'), v6_sha256: createHash('sha256').update(v6Bytes).digest('hex'), measurement_source_hash: v5.input.measurement_source_hash},
  dimensions: {
    fixtures: contract.length,
    routes: projection.length,
    features: features.length,
    languages: languages.length,
    lexical_axes: axes.length,
    prior_columns: oldDesign[0].length,
    residual_columns: sourceResidual[0].length,
    expanded_columns: expandedColumns,
    expanded_cells: projection.length * expandedColumns,
    residual_gram_cells: newNewGram.length * newNewGram[0].length,
    old_residual_cross_cells: oldNewGram.length * oldNewGram[0].length,
    reconstructed_full_gram_cells: expandedColumns * expandedColumns,
  },
  axes: {features, languages, lexical_axes: axes, residual_columns: columns},
  matrices: {
    feature_lexical_mean: featureAxisMean,
    fixture_lexical_expected: fixtureExpected,
    fixture_lexical_residual: fixtureResidual,
    route_lexical_residual: routeResidual,
    source_lexical_residual_interaction: sourceResidual,
    prior_residual_cross_gram: oldNewGram,
    residual_gram: newNewGram,
  },
  vectors: {residual_failure_projection: residualFailure, residual_known_energy: residualKnownEnergy, standardized_influence: residualInfluence},
  summary: {top_residual_influences: ranked.slice(0, 60)},
  rules: [
    'The feature by lexical-axis mean is Contract transpose times Lexical divided by the exact fixture count per feature.',
    'Residual equals observed lexical vector minus its feature mean vector.',
    'Source by residual interaction is a row-wise Kronecker product; no language-specific parser rule is added.',
    'The V7 Gram matrix is exact when the V6 Gram, prior-residual cross block, its transpose, and residual Gram are assembled.',
    'Standardized influence is the absolute failure projection divided by the square root of known residual energy; no priority weights are used.',
  ],
};
await fs.writeFile(path.join(outDir, 'semantic_matrix_v7.json'), JSON.stringify(result));
const summary = {schema_version: result.schema_version, input: result.input, dimensions: result.dimensions, summary: result.summary, rules: result.rules};
await fs.writeFile(path.join(outDir, 'semantic_matrix_v7_summary.json'), JSON.stringify(summary, null, 2));
console.log(JSON.stringify(summary, null, 2));
