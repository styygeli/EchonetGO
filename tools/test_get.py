import socket
import sys

ip = sys.argv[1]
port = int(sys.argv[2])
eoj = bytes.fromhex(sys.argv[3])
req = bytes.fromhex('1081000105ff01') + eoj + bytes.fromhex('62018000')
sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
if port > 0:
    sock.bind(('0.0.0.0', port))
sock.settimeout(2)
try:
    sock.sendto(req, (ip, 3610))
    data, addr = sock.recvfrom(2048)
    print("Reply from:", addr, data.hex())
except Exception as e:
    print("Error:", e)
