// Punto de entrada del paquete que usa el panel para hablar con un firmador remoto (NIP-46): solo lo imprescindible de
// nostr-tools (claves, firma y verificación de eventos, y cifrado NIP-44). Se reconstruye con scripts/build-admin-vendor.sh.
export { generateSecretKey, getPublicKey, finalizeEvent, verifyEvent } from 'nostr-tools/pure'
export * as nip44 from 'nostr-tools/nip44'
export { bytesToHex, hexToBytes } from '@noble/hashes/utils'
