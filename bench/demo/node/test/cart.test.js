import { describe, expect, it } from "vitest";
import { applyDiscount, formatCents, subtotal } from "../src/cart.js";

describe("subtotal", () => {
  it.each([
    { name: "empty cart", lines: [], want: 0 },
    { name: "one line", lines: [{ price: 250, qty: 2 }], want: 500 },
    { name: "two lines", lines: [{ price: 199, qty: 3 }, { price: 1000, qty: 1 }], want: 1597 },
  ])("$name", ({ lines, want }) => {
    expect(subtotal(lines)).toBe(want);
  });

  it("rejects a fractional price", () => {
    expect(() => subtotal([{ price: 1.5, qty: 1 }])).toThrow(RangeError);
  });

  it("rejects a zero quantity", () => {
    expect(() => subtotal([{ price: 100, qty: 0 }])).toThrow(RangeError);
  });
});

describe("applyDiscount", () => {
  it.each([
    { amount: 1000, percent: 0, want: 1000 },
    { amount: 1000, percent: 15, want: 850 },
    { amount: 999, percent: 50, want: 500 },
    { amount: 1000, percent: 100, want: 0 },
  ])("$percent% off $amount", ({ amount, percent, want }) => {
    expect(applyDiscount(amount, percent)).toBe(want);
  });

  it("rejects a discount above 100%", () => {
    expect(() => applyDiscount(1000, 101)).toThrow(RangeError);
  });
});

describe("formatCents", () => {
  it("renders two decimals", () => {
    expect(formatCents(1234)).toBe("12.34 EUR");
    expect(formatCents(5)).toBe("0.05 EUR");
  });
});
