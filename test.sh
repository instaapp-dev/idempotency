# Idempotency Key
IK=$(openssl rand -hex 32)
echo "Create song with IK=$IK"
# Sending consecutive requests.
curl -i -H "Idempotency-Key:$IK" -X POST -d '{"title": "test", "artist": "test", "year": 2021}' localhost:8080/songs/create &
curl -i -H "Idempotency-Key:$IK" -X POST -d '{"title": "test", "artist": "test", "year": 2021}' localhost:8080/songs/create &
curl -i -H "Idempotency-Key:$IK" -X POST -d '{"title": "test", "artist": "test", "year": 2021}' localhost:8080/songs/create

# Wait for create song done for better output.
sleep 1
echo "Get songs"
curl -i localhost:8080/songs/list
