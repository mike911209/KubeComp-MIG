import os
import threading
import time
from queue import Queue
from flask import Flask, request, jsonify
from transformers import pipeline
import random
import string
from prometheus_client import start_http_server, Gauge
from loguru import logger
import signal

def signal_handler(sig, frame):
    logger.info("Exiting...")
    os._exit(0)

signal.signal(signal.SIGTERM, signal_handler)

mean_time_per_token_metrics = Gauge('mean_time_per_token', 'mean time per token of inference')

last_empty = False

app = Flask(__name__)

MODEL_NAME = os.getenv("MODEL_NAME", "openai-community/gpt2")
MAX_BATCH_SIZE = int(os.getenv("MAX_BATCH_SIZE", "50"))
BATCH_TIME = float(os.getenv("BATCH_TIME","0.1"))
TIMEOUT = float(os.getenv("TIMEOUT","60"))

# MODEL_NAME = "gpt2"
# MAX_BATCH_SIZE = 20
# BATCH_TIME = 0.01
# TIMEOUT = 60

model = pipeline("text-generation", model=MODEL_NAME, truncation=True)

input_queue = Queue()
output_queue = {}

def generate_hash():
    return ''.join(random.choices(string.ascii_letters + string.digits, k=10))

def process_queue():
    global last_empty
    while True:
        time.sleep(BATCH_TIME)
        if not input_queue.empty():
            last_empty = False
            batch_size = min(input_queue.qsize(), MAX_BATCH_SIZE)
            logger.info(f"Processing batch of size {batch_size}")
            batch_id, batch_text = [], []
            for _ in range(batch_size):
                request_id, request_text = input_queue.get()
                batch_id.append(request_id)
                batch_text.append(request_text)
            try:
                time_start = time.time()
                result = model(batch_text, max_length=50, pad_token_id=model.tokenizer.eos_token_id)
                time_end = time.time()
                total_tokens = sum(len(model.tokenizer(res[0]["generated_text"])["input_ids"]) for res in result)
                mean_time_per_token = (time_end - time_start) / total_tokens
                mean_time_per_token_metrics.set(mean_time_per_token)
                logger.success(f"Total tokens: {total_tokens}, mean time per token: {mean_time_per_token}")
                for i, res in enumerate(result):
                    output_queue[batch_id[i]] = res
            except Exception as e:
                for i, res in enumerate(result):
                    output_queue[batch_id[i]] = {"error": str(e)}
        else:
            if not last_empty:
                logger.info("Queue is empty")
            mean_time_per_token_metrics.set(0)
            last_empty = True
        

worker_thread = threading.Thread(target=process_queue, daemon=True)
worker_thread.start()

@app.route("/generate", methods=["POST"])
def predict():
    try:
        data = request.json
        if "inputs" not in data:
            return jsonify({"error": "Missing inputs"}), 400
        input_text = data["inputs"]
        request_id = generate_hash()
        input_queue.put((request_id, input_text))
        timeout = TIMEOUT
        while timeout > 0:
            if request_id in output_queue:
                response = output_queue.pop(request_id)
                return jsonify(response), 200
            time.sleep(0.1)
            timeout -= 0.1
        return jsonify({"error": "Timeout"}), 500
    except Exception as e:
        return jsonify({"error": str(e)}), 500

if __name__ == "__main__":
    start_http_server(8000)
    app.run(host="0.0.0.0", port=8080)
