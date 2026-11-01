import { useMemo } from 'react';

const capacities = [
  { version: 1, data: 19, ecc: 7 },
  { version: 2, data: 34, ecc: 10 },
  { version: 3, data: 55, ecc: 15 },
  { version: 4, data: 80, ecc: 20 },
  { version: 5, data: 108, ecc: 26 },
];

function multiplyGF(a, b) {
  let result = 0;
  for (let i = 0; i < 8; i += 1) {
    if (b & 1) result ^= a;
    const high = a & 0x80;
    a = (a << 1) & 0xff;
    if (high) a ^= 0x1d;
    b >>= 1;
  }
  return result;
}

function reedSolomon(data, degree) {
  const divisor = new Uint8Array(degree);
  divisor[degree - 1] = 1;
  let root = 1;
  for (let i = 0; i < degree; i += 1) {
    for (let j = 0; j < degree; j += 1) {
      divisor[j] = multiplyGF(divisor[j], root);
      if (j + 1 < degree) divisor[j] ^= divisor[j + 1];
    }
    root = multiplyGF(root, 2);
  }
  const remainder = new Uint8Array(data.length + degree);
  remainder.set(data);
  for (let i = 0; i < data.length; i += 1) {
    const factor = remainder[i];
    if (factor) {
      for (let j = 0; j < divisor.length; j += 1) remainder[i + j] ^= multiplyGF(divisor[j], factor);
    }
  }
  return remainder.slice(data.length);
}

function appendBits(target, value, count) {
  for (let i = count - 1; i >= 0; i -= 1) target.push((value >>> i) & 1);
}

function makeData(text) {
  const bytes = new TextEncoder().encode(text);
  const spec = capacities.find(({ data }) => bytes.length <= data - 2);
  if (!spec || bytes.length > 106) throw new Error('The share link is too long to encode.');

  const capacityBits = spec.data * 8;
  const bits = [];
  appendBits(bits, 0b0100, 4);
  appendBits(bits, bytes.length, 8);
  for (const byte of bytes) appendBits(bits, byte, 8);
  appendBits(bits, 0, Math.min(4, capacityBits - bits.length));
  while (bits.length % 8) bits.push(0);

  const data = [];
  for (let i = 0; i < bits.length; i += 8) {
    let byte = 0;
    for (let j = 0; j < 8; j += 1) byte = (byte << 1) | bits[i + j];
    data.push(byte);
  }
  for (let pad = 0; data.length < spec.data; pad += 1) data.push(pad % 2 === 0 ? 0xec : 0x11);
  const codewords = [...data, ...reedSolomon(data, spec.ecc)];
  return { version: spec.version, bits: codewords.flatMap((byte) => Array.from({ length: 8 }, (_, i) => (byte >>> (7 - i)) & 1)) };
}

function makeMatrix(text) {
  const { version, bits } = makeData(text);
  const size = version * 4 + 17;
  const matrix = Array.from({ length: size }, () => Array(size).fill(false));
  const functionModules = Array.from({ length: size }, () => Array(size).fill(false));
  const setFunction = (x, y, dark) => {
    if (x >= 0 && y >= 0 && x < size && y < size) {
      matrix[y][x] = dark;
      functionModules[y][x] = true;
    }
  };

  function drawFinder(centerX, centerY) {
    for (let dy = -4; dy <= 4; dy += 1) {
      for (let dx = -4; dx <= 4; dx += 1) {
        const distance = Math.max(Math.abs(dx), Math.abs(dy));
        setFunction(centerX + dx, centerY + dy, distance !== 2 && distance !== 4);
      }
    }
  }

  drawFinder(3, 3);
  drawFinder(size - 4, 3);
  drawFinder(3, size - 4);

  if (version >= 2) {
    const center = size - 7;
    for (let dy = -2; dy <= 2; dy += 1) {
      for (let dx = -2; dx <= 2; dx += 1) {
        setFunction(center + dx, center + dy, Math.max(Math.abs(dx), Math.abs(dy)) !== 1);
      }
    }
  }

  for (let i = 8; i < size - 8; i += 1) {
    setFunction(i, 6, i % 2 === 0);
    setFunction(6, i, i % 2 === 0);
  }

  const formatData = (0b01 << 3) | 0;
  let remainder = formatData;
  for (let i = 0; i < 10; i += 1) remainder = (remainder << 1) ^ ((remainder >>> 9) * 0x537);
  const formatBits = ((formatData << 10) | remainder) ^ 0x5412;
  const bit = (index) => ((formatBits >>> index) & 1) !== 0;
  for (let i = 0; i <= 5; i += 1) setFunction(8, i, bit(i));
  setFunction(8, 7, bit(6));
  setFunction(8, 8, bit(7));
  setFunction(7, 8, bit(8));
  for (let i = 9; i < 15; i += 1) setFunction(14 - i, 8, bit(i));
  for (let i = 0; i < 8; i += 1) setFunction(size - 1 - i, 8, bit(i));
  for (let i = 8; i < 15; i += 1) setFunction(8, size - 15 + i, bit(i));
  setFunction(8, size - 8, true);

  let bitIndex = 0;
  let upward = true;
  for (let right = size - 1; right >= 1; right -= 2) {
    if (right === 6) right = 5;
    for (let vertical = 0; vertical < size; vertical += 1) {
      const y = upward ? size - 1 - vertical : vertical;
      for (let offset = 0; offset < 2; offset += 1) {
        const x = right - offset;
        if (functionModules[y][x]) continue;
        const value = bitIndex < bits.length ? bits[bitIndex] === 1 : false;
        matrix[y][x] = value !== ((x + y) % 2 === 0);
        bitIndex += 1;
      }
    }
    upward = !upward;
  }
  return matrix;
}

export default function QRCode({ value }) {
  const matrix = useMemo(() => makeMatrix(value), [value]);
  const quiet = 4;
  const dimension = matrix.length + quiet * 2;
  let path = '';
  matrix.forEach((row, y) => row.forEach((dark, x) => {
    if (dark) path += `M${x + quiet},${y + quiet}h1v1h-1z`;
  }));

  return (
    <svg className="share-qr" viewBox={`0 0 ${dimension} ${dimension}`} width={size} height={size} role="img" aria-label="QR code for the poll share link" shapeRendering="crispEdges">
      <rect className="qr-background" width={dimension} height={dimension} />
      <path className="qr-modules" d={path} />
    </svg>
  );
}
