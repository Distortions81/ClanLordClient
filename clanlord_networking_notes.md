**Clan Lord Networking Protocol Notes for Custom Client Development**

---

**Overview:**
Clan Lord, developed by Delta Tao Software, uses a custom client-server networking protocol involving both TCP and UDP traffic on port 5010. A successful custom client implementation must understand the login sequence, post-login requirements, and packet structure.

---

**Connection Summary:**
- **Server address:** `server.clanlord.com`
- **Port:** TCP/UDP 5010

---

**1. Login Phase (TCP):**
- Client connects to `server.clanlord.com:5010` via TCP.
- Sends serial number and authentication data.
- Upon success, the server sends character/account info.
- TCP remains open for future critical communications.

**Important:** A successful TCP login does **not** imply the client is "connected" in-game. The game expects a valid UDP message next.

---

**2. Post-Login (UDP Required):**
- The client must open a **UDP socket** from an ephemeral port and send **immediately** to `server.clanlord.com:5010`.
- The server will not mark the character as "connected" until it receives a valid UDP packet.
- Clients that don't send valid UDP messages will appear "disconnected" in-game.

---

**3. UDP Packet Expectations:**
- Simple test packets like `0xFF 0xFF` are **not sufficient**.
- First packet should include a **recognized opcode** (e.g., movement, ready signal) and possibly character/session identifiers.

**Example placeholder formats:**
- `[]byte{0x00}` — possible noop or keepalive
- `[]byte{'M', 0x00, 0x00, 0x00, 0x00, 0x01}` — move opcode with coordinates and facing

The exact structure depends on the client version. Referencing open-source alternate clients is essential.

---

**4. Connection Maintenance:**
- UDP packets must be sent periodically to avoid NAT/firewall timeouts (typically every few seconds).
- Many unofficial clients use pings or periodic updates to maintain presence.

---

**5. Alternate Client Insights:**
- **Clanlord Java Client (GitLab)**:
  - Open-source and actively maintained
  - Shows how initial UDP messages are constructed
  - Good reference for opcode and packet format

- **Clieunk (Mac Client):**
  - Closed source but functional and protocol-compatible
  - Shows that the protocol can be reverse-engineered

- **YappyGM's GitHub Client:**
  - Prototype only; lacks full networking implementation

- **Gorvin's Reverse Engineered Notes (Archived):**
  - Contains documentation of message types and structure

---

**6. Recommendations for Custom Client:**
- After TCP login, open a UDP socket and send a valid opcode-based packet immediately.
- Use Wireshark to observe packet content from the official client.
- Refer to the Java client’s code to model UDP packet construction.
- Continue sending UDP packets to avoid being disconnected.

---

**Tools:**
- Wireshark (`udp.port == 5010`) to inspect traffic
- GitLab repo: https://gitlab.com/clanlord/clanlord-java-client
- Archive.org for Gorvin's notes (search: "Gorvin Clan Lord Reverse Engineered")

---

**Next Steps:**
- Capture and replicate the first UDP message after login.
- Implement regular keepalive or command updates.
- Continue exploring reverse-engineered protocol documentation.

