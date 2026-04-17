// Minimal ZIP builder — no external dependency. Produces a valid
// ZIP archive from an array of {name, content} text entries.
export function buildZip(files: { name: string; content: string }[]): Uint8Array {
  const entries: { header: Uint8Array; data: Uint8Array; name: Uint8Array; offset: number }[] = []
  let offset = 0

  for (const f of files) {
    const nameBytes = new TextEncoder().encode(f.name)
    const dataBytes = new TextEncoder().encode(f.content)
    const crc = crc32(dataBytes)
    const header = new Uint8Array(30 + nameBytes.length)
    const dv = new DataView(header.buffer)
    dv.setUint32(0, 0x04034b50, true)
    dv.setUint16(4, 20, true)
    dv.setUint16(6, 0, true)
    dv.setUint16(8, 0, true)
    dv.setUint16(10, 0, true)
    dv.setUint16(12, 0, true)
    dv.setUint32(14, crc, true)
    dv.setUint32(18, dataBytes.length, true)
    dv.setUint32(22, dataBytes.length, true)
    dv.setUint16(26, nameBytes.length, true)
    dv.setUint16(28, 0, true)
    header.set(nameBytes, 30)
    entries.push({ header, data: dataBytes, name: nameBytes, offset })
    offset += header.length + dataBytes.length
  }

  const cdParts: Uint8Array[] = []
  for (const e of entries) {
    const cd = new Uint8Array(46 + e.name.length)
    const dv = new DataView(cd.buffer)
    dv.setUint32(0, 0x02014b50, true)
    dv.setUint16(4, 20, true)
    dv.setUint16(6, 20, true)
    dv.setUint16(8, 0, true)
    dv.setUint16(10, 0, true)
    dv.setUint16(12, 0, true)
    dv.setUint16(14, 0, true)
    dv.setUint32(16, new DataView(e.header.buffer).getUint32(14, true), true)
    dv.setUint32(20, e.data.length, true)
    dv.setUint32(24, e.data.length, true)
    dv.setUint16(28, e.name.length, true)
    dv.setUint16(30, 0, true)
    dv.setUint16(32, 0, true)
    dv.setUint16(34, 0, true)
    dv.setUint16(36, 0, true)
    dv.setUint32(38, 0, true)
    dv.setUint32(42, e.offset, true)
    cd.set(e.name, 46)
    cdParts.push(cd)
  }
  const cdSize = cdParts.reduce((s, p) => s + p.length, 0)

  const eocd = new Uint8Array(22)
  const edv = new DataView(eocd.buffer)
  edv.setUint32(0, 0x06054b50, true)
  edv.setUint16(4, 0, true)
  edv.setUint16(6, 0, true)
  edv.setUint16(8, entries.length, true)
  edv.setUint16(10, entries.length, true)
  edv.setUint32(12, cdSize, true)
  edv.setUint32(16, offset, true)
  edv.setUint16(20, 0, true)

  const total = offset + cdSize + 22
  const out = new Uint8Array(total)
  let pos = 0
  for (const e of entries) {
    out.set(e.header, pos); pos += e.header.length
    out.set(e.data, pos); pos += e.data.length
  }
  for (const cd of cdParts) {
    out.set(cd, pos); pos += cd.length
  }
  out.set(eocd, pos)
  return out
}

function crc32(data: Uint8Array): number {
  let crc = 0xFFFFFFFF
  for (let i = 0; i < data.length; i++) {
    crc ^= data[i]
    for (let j = 0; j < 8; j++) {
      crc = (crc >>> 1) ^ (crc & 1 ? 0xEDB88320 : 0)
    }
  }
  return (crc ^ 0xFFFFFFFF) >>> 0
}
