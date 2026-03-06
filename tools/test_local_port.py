import socket

def test_bind(port):
    try:
        sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
        sock.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
        sock.bind(('0.0.0.0', port))
        print(f"Successfully bound to {port}")
    except Exception as e:
        print(f"Failed to bind {port}: {e}")

test_bind(3610)
