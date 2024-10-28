import argparse
import os
import requests
import time
import random
import math
#import matplotlib.pyplot as plt
import numpy as np

URL = "http://localhost:8080"
#URL = "https://echo.free.beeceptor.com"

def sleep(interval: float):
	timer = time.time()
	while timer + interval > time.time():
		pass

# class RequestTracker:
# 	def __init__(self):
# 		self.timestamps = []

# 	def log_request(self):
# 		self.timestamps.append(time.time())

# 	def plot_rate(self):
# 		if len(self.timestamps) < 2:
# 			print("Not enough data to plot rate.")
# 			return

# 		start_time = self.timestamps[0]
# 		relative_times = [t - start_time for t in self.timestamps]
# 		rates = [1 / (relative_times[i] - relative_times[i - 1]) for i in range(1, len(relative_times))]

# 		plt.figure(figsize=(10, 5))
# 		plt.plot(relative_times[1:], rates, marker='o')
# 		plt.xlabel('Time (seconds)')
# 		plt.ylabel('Rate (requests per second)')
# 		plt.title('Request Rate Diagram')
# 		plt.grid(True)
# 		plt.show()

class RequestSender:
	def __init__(self, models, ratios, prompt, max_new_tokens, show_body, slo):
		self.models = models
		self.ratios = ratios
		self.prompt = prompt
		self.max_new_tokens = max_new_tokens
		self.show_body = show_body
		self.slo = slo
		# self.tracker = RequestTracker()

	def send_request(self, model):
		json_data = {
			"token": str(self.prompt),
			"par": {
				"max_new_tokens": str(self.max_new_tokens)
			},
			"env": {
				"MODEL_ID": model,
				"HF_TOKEN": os.getenv("HF_TOKEN")
			},
			"label": {"slo": str(self.slo)}
		}

		headers = {
			"Content-Type": "application/json",
			"Host": "dispatcher.default.127.0.0.1.nip.io"
		}
		
		response = requests.post(URL, headers=headers, json=json_data)
		if self.show_body:
			print(response.text)

		if response.status_code != 200 and response.status_code != 202:
			print(f"Error sending request: {response.status_code}")
			return False

		# self.tracker.log_request()
		return True

	def select_model(self):
		return random.choices(self.models, weights=self.ratios, k=1)[0]

	def constant_rate(self, total_time, interval=3):
		start_time = time.time()
		request_count = 0
		while time.time() - start_time < total_time:
			model = self.select_model()
			print(f"Sending request at {time.time() - start_time:.2f} seconds")
			if not self.send_request(model):
				print(f"Total requests sent: {request_count}")
				self.tracker.plot_rate()
				return
			request_count += 1
			sleep(interval)
		print(f"Total requests sent: {request_count}")
		

	def oscillation_rate(self, total_time, max_interval, min_interval, period):
		start_time = time.time()
		request_count = 0
		
		while time.time() - start_time < total_time:
			model = self.select_model()
			print(f"Sending request at {time.time() - start_time:.2f} seconds")
			if not self.send_request(model):
				break
			request_count += 1
			interval = min_interval + (max_interval - min_interval) * (1 + math.sin(2 * math.pi * time.time() / period)) / 2
			sleep(interval)
		print(f"Total requests sent: {request_count}")
	

	def hybrid_rate(self, total_time, max_interval, min_interval, interval):
		start_time = time.time()
		request_count = 0
		i = 0
		while time.time() - start_time < total_time:
			model = self.select_model()
			print(f"Sending request at {time.time() - start_time:.2f} seconds")
			if not self.send_request(model):
				break
			request_count += 1
			if i % 2 == 0:
				sleep(interval)
			else:
				sleep(random.uniform(min_interval, max_interval))
			i += 1
		print(f"Total requests sent: {request_count}")
		

	def random_rate(self, total_time, max_interval, min_interval):
		start_time = time.time()
		request_count = 0
		while time.time() - start_time < total_time:
			model = self.select_model()
			print(f"Sending request at {time.time() - start_time:.2f} seconds")
			if not self.send_request(model):
				break
			request_count += 1
			sleep(random.uniform(min_interval, max_interval))
		print(f"Total requests sent: {request_count}")
		

	def linear_rate(self, total_time, min_interval, slope):
		start_time = time.time()
		request_count = 0
		while time.time() - start_time < total_time:
			model = self.select_model()
			print(f"Sending request at {time.time() - start_time:.2f} seconds")
			if not self.send_request(model):
				break
			request_count += 1
			interval = min_interval + slope * (time.time() -start_time)
			sleep(max(0, interval))
		print(f"Total requests sent: {request_count}")
		

	def poisson_rate(self,total_time,rate =1):
	    
		intervals = np.arange(0, total_time, 1)
		request_counts = np.random.poisson(rate, total_time)
		
		start_time = time.time()
		request_count = 0
		for interval, count in zip(intervals, request_counts):
			for _ in range(count):
				model = self.select_model()
				print(f"Sending request at {time.time() - start_time:.2f} seconds")
				if not self.send_request(model):
					break
				request_count += 1
			sleep(1)
		print(f"Total requests sent: {request_count}")
		
		


def main():
	models = [
		"meta-llama/Meta-Llama-3.1-8B",
		"meta-llama/Llama-3.2-1B-Instruct",
		"openai-community/gpt2",
	]
	parser = argparse.ArgumentParser(description="Send requests to a model",formatter_class=argparse.RawTextHelpFormatter)
	model_choices = {i: model for i, model in enumerate(models)}
	parser.add_argument("models", type=int, nargs='+', default=[0], choices=list(model_choices.keys()), help=f"Models to use {model_choices}")
	parser.add_argument("total_time", type=int, help="Total time to send requests (in seconds)")
	parser.add_argument("slo", type=float, default = 5 , help="User slo")
	parser.add_argument("type", type=str, choices=["const", "osc", "hybrid", "rand", "linear", "poi"], help="""
					 Type of workload function to use : 
					 constant (constant worloads): requires -interval 
					 osc (oscillation , in sin wave): requires --max_interval, --min_interval, --period 
					 hybrid (random + constant): requires --max_interval, --min_interval, --interval 
					 rand (random in a range): requires --max_interval, --min_interval 
					 linear (linear increase workload): requires --min_interval, --slope 
					 poi (poisson distribution): requires --rate
""")
	parser.add_argument("-r", "--ratios", type=float, nargs='+', help="Model ratios")
	parser.add_argument("-p", "--prompt", type=str, default="What is Deep Learning?", help="Prompt")
	parser.add_argument("-t", "--tokens", type=int, default=1000, help="Max new tokens")
	parser.add_argument("-s", "--show", action="store_true", help="Show response body")
	parser.add_argument("-sp","--show-plot", action="store_true", help="Show plot")
	parser.add_argument("-i", "--interval", type=float, default=1.0, help="Interval for constant rate")
	parser.add_argument("-max", "--max_interval", type=float, default=5.0, help="Max interval")
	parser.add_argument("-max_t", "--max_new_tokens", type=int , default=500 , help="Max tokens to generate")
	parser.add_argument("-min", "--min_interval", type=float, default=0.5, help="Min interval")
	parser.add_argument("-per", "--period", type=int, default=10, help="Oscillation period")
	parser.add_argument("-rt", "--rate", type=float, default=1, help="Request rate")
	parser.add_argument("-sl", "--slope", type=float, default=0.1, help="Slope for linear rate")
	args = parser.parse_args()
	if args.ratios is None:
		args.ratios = [1 / len(args.models) for _ in args.models]

	selected_models = [models[i] for i in args.models]

	sender = RequestSender(selected_models, args.ratios, args.prompt, args.tokens, args.show, args.slo)

	if args.type == "const":
		sender.constant_rate(args.total_time, args.interval)
	elif args.type == "osc":
		sender.oscillation_rate(args.total_time, args.max_interval, args.min_interval, args.period)
	elif args.type == "hybrid":
		sender.hybrid_rate(args.total_time, args.max_interval, args.min_interval, args.interval)
	elif args.type == "rand":
		sender.random_rate(args.total_time, args.max_interval, args.min_interval)
	elif args.type == "linear":
		sender.linear_rate(args.total_time, args.min_interval, args.slope)
	elif args.type == "poi":
		sender.poisson_rate(args.total_time, args.rate)
	else:
		sender.constant_rate(args.total_time, args.interval)

	# if args.show_plot:
	# 	sender.tracker.plot_rate()

if __name__ == "__main__":
	main()
