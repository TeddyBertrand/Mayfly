// Prices are integer cents, so totals never pick up floating-point rounding errors.

/**
 * subtotal returns the sum of price times quantity over the cart lines, in cents.
 * @param {{price: number, qty: number}[]} lines
 */
export function subtotal(lines) {
  return lines.reduce((sum, { price, qty }) => {
    if (!Number.isInteger(price) || price < 0) throw new RangeError(`invalid price: ${price}`);
    if (!Number.isInteger(qty) || qty < 1) throw new RangeError(`invalid quantity: ${qty}`);
    return sum + price * qty;
  }, 0);
}

/**
 * applyDiscount returns amount reduced by percent, rounded to the nearest cent.
 * @param {number} amount in cents
 * @param {number} percent between 0 and 100
 */
export function applyDiscount(amount, percent) {
  if (percent < 0 || percent > 100) throw new RangeError(`invalid discount: ${percent}`);
  return Math.round(amount * (1 - percent / 100));
}

/**
 * formatCents renders an amount in cents as a euro string, e.g. 1234 -> "12.34 EUR".
 * @param {number} cents
 */
export function formatCents(cents) {
  return `${(cents / 100).toFixed(2)} EUR`;
}
