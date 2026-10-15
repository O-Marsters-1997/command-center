declare module "bun:test" {
  export function describe(name: string, run: () => void): void;
  export function test(name: string, run: () => void | Promise<void>): void;
  export function expect<T>(value: T): {
    toBe(expected: T): void;
    toEqual(expected: unknown): void;
    toBeNull(): void;
    toContain(expected: string): void;
    toStartWith(expected: string): void;
  };
}
