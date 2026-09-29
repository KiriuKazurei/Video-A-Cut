/** Convert a JSON number's decimal representation into a positive rational. */
function fraction(value) {
  if (!Number.isFinite(value) || value < 0) throw new Error(`invalid time value: ${value}`);
  const [mantissa, exponent = '0'] = String(value).toLowerCase().split('e');
  const [whole, decimal = ''] = mantissa.split('.');
  let numerator = BigInt(`${whole}${decimal}`);
  let denominator = 10n ** BigInt(decimal.length);
  const power = Number(exponent);
  if (power >= 0) numerator *= 10n ** BigInt(power);
  else denominator *= 10n ** BigInt(-power);
  return { numerator, denominator };
}

/** Non-negative seconds to an integer frame/sample boundary, half-up rounding. */
export function toUnits(seconds, unitsPerSecond) {
  const time = fraction(seconds);
  const units = fraction(unitsPerSecond);
  const numerator = time.numerator * units.numerator;
  const denominator = time.denominator * units.denominator;
  const result = (2n * numerator + denominator) / (2n * denominator);
  if (result > BigInt(Number.MAX_SAFE_INTEGER)) throw new Error('time value exceeds safe integer range');
  return Number(result);
}
