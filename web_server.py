#!/usr/bin/env python3

import json
import sqlite3
import http.server
import socketserver
from urllib.parse import urlparse, parse_qs
import os
from datetime import datetime

class SWMHandler(http.server.SimpleHTTPRequestHandler):
    
    def do_GET(self):
        parsed_path = urlparse(self.path)
        
        if parsed_path.path == '/':
            self.serve_home()
        elif parsed_path.path == '/api/pools':
            self.get_pools()
        elif parsed_path.path == '/api/data':
            self.get_historical_data(parsed_path)
        else:
            super().do_GET()
    
    def serve_home(self):
        html = """
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>SWM Pool Utility Monitor</title>
    <script src="https://cdn.jsdelivr.net/npm/chart.js"></script>
    <style>
        body {
            font-family: Arial, sans-serif;
            margin: 0;
            padding: 20px;
            background-color: #f5f5f5;
        }
        .container {
            max-width: 1200px;
            margin: 0 auto;
            background: white;
            padding: 20px;
            border-radius: 8px;
            box-shadow: 0 2px 4px rgba(0,0,0,0.1);
        }
        h1 {
            color: #333;
            text-align: center;
            margin-bottom: 30px;
        }
        .controls {
            display: flex;
            gap: 20px;
            margin-bottom: 30px;
            flex-wrap: wrap;
        }
        .control-group {
            flex: 1;
            min-width: 200px;
        }
        label {
            display: block;
            margin-bottom: 5px;
            font-weight: bold;
        }
        select, input {
            width: 100%;
            padding: 8px;
            border: 1px solid #ddd;
            border-radius: 4px;
        }
        button {
            background-color: #007bff;
            color: white;
            padding: 10px 20px;
            border: none;
            border-radius: 4px;
            cursor: pointer;
            font-size: 16px;
        }
        button:hover {
            background-color: #0056b3;
        }
        .chart-container {
            position: relative;
            height: 400px;
            margin-top: 20px;
        }
        .stats {
            display: grid;
            grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
            gap: 20px;
            margin-top: 30px;
        }
        .stat-card {
            background: #f8f9fa;
            padding: 15px;
            border-radius: 6px;
            text-align: center;
        }
        .stat-value {
            font-size: 24px;
            font-weight: bold;
            color: #007bff;
        }
        .stat-label {
            font-size: 14px;
            color: #666;
            margin-top: 5px;
        }
        .loading {
            text-align: center;
            padding: 20px;
            color: #666;
        }
    </style>
</head>
<body>
    <div class="container">
        <h1>SWM Pool Utility Monitor</h1>
        
        <div class="controls">
            <div class="control-group">
                <label for="poolSelect">Select Pool:</label>
                <select id="poolSelect" multiple>
                    <option value="">All Pools</option>
                </select>
            </div>
            <div class="control-group">
                <label for="startDate">Start Date:</label>
                <input type="date" id="startDate">
            </div>
            <div class="control-group">
                <label for="endDate">End Date:</label>
                <input type="date" id="endDate">
            </div>
            <div class="control-group">
                <label>&nbsp;</label>
                <button onclick="loadData()">Load Data</button>
            </div>
        </div>

        <div class="chart-container">
            <canvas id="utilityChart"></canvas>
        </div>

        <div class="stats" id="stats">
            <div class="stat-card">
                <div class="stat-value" id="avgUtility">-</div>
                <div class="stat-label">Average Utility</div>
            </div>
            <div class="stat-card">
                <div class="stat-value" id="maxUtility">-</div>
                <div class="stat-label">Peak Utility</div>
            </div>
            <div class="stat-card">
                <div class="stat-value" id="minUtility">-</div>
                <div class="stat-label">Minimum Utility</div>
            </div>
            <div class="stat-card">
                <div class="stat-value" id="dataPoints">-</div>
                <div class="stat-label">Data Points</div>
            </div>
        </div>
    </div>

    <script>
        let chart = null;

        async function loadPools() {
            try {
                const response = await fetch('/api/pools');
                const pools = await response.json();
                const select = document.getElementById('poolSelect');
                
                pools.forEach(pool => {
                    const option = document.createElement('option');
                    option.value = pool;
                    option.textContent = pool;
                    select.appendChild(option);
                });
            } catch (error) {
                console.error('Error loading pools:', error);
            }
        }

        async function loadData() {
            const poolSelect = document.getElementById('poolSelect');
            const selectedPools = Array.from(poolSelect.selectedOptions).map(option => option.value);
            const startDate = document.getElementById('startDate').value;
            const endDate = document.getElementById('endDate').value;

            try {
                const params = new URLSearchParams();
                if (selectedPools.length > 0 && !selectedPools.includes('')) {
                    params.append('pools', selectedPools.join(','));
                }
                if (startDate) params.append('start', startDate);
                if (endDate) params.append('end', endDate);

                const response = await fetch(`/api/data?${params}`);
                const data = await response.json();
                
                updateChart(data);
                updateStats(data);
            } catch (error) {
                console.error('Error loading data:', error);
            }
        }

        function updateChart(data) {
            const ctx = document.getElementById('utilityChart').getContext('2d');
            
            if (chart) {
                chart.destroy();
            }

            const colors = [
                '#FF6384', '#36A2EB', '#FFCE56', '#4BC0C0', '#9966FF',
                '#FF9F40', '#FF6384', '#C9CBCF', '#4BC0C0', '#FF6384'
            ];

            chart = new Chart(ctx, {
                type: 'line',
                data: data,
                options: {
                    responsive: true,
                    maintainAspectRatio: false,
                    plugins: {
                        title: {
                            display: true,
                            text: 'Pool Utility Over Time'
                        },
                        legend: {
                            display: true,
                            position: 'top'
                        }
                    },
                    scales: {
                        y: {
                            beginAtZero: true,
                            max: 100,
                            title: {
                                display: true,
                                text: 'Utility (%)'
                            }
                        },
                        x: {
                            title: {
                                display: true,
                                text: 'Time'
                            }
                        }
                    }
                }
            });
        }

        function updateStats(data) {
            if (!data.datasets || data.datasets.length === 0) {
                document.getElementById('avgUtility').textContent = '-';
                document.getElementById('maxUtility').textContent = '-';
                document.getElementById('minUtility').textContent = '-';
                document.getElementById('dataPoints').textContent = '-';
                return;
            }

            let allValues = [];
            let totalPoints = 0;

            data.datasets.forEach(dataset => {
                allValues = allValues.concat(dataset.data);
                totalPoints += dataset.data.length;
            });

            if (allValues.length === 0) {
                document.getElementById('avgUtility').textContent = '-';
                document.getElementById('maxUtility').textContent = '-';
                document.getElementById('minUtility').textContent = '-';
                document.getElementById('dataPoints').textContent = '0';
                return;
            }

            const avg = Math.round(allValues.reduce((a, b) => a + b, 0) / allValues.length);
            const max = Math.max(...allValues);
            const min = Math.min(...allValues);

            document.getElementById('avgUtility').textContent = avg + '%';
            document.getElementById('maxUtility').textContent = max + '%';
            document.getElementById('minUtility').textContent = min + '%';
            document.getElementById('dataPoints').textContent = totalPoints;
        }

        // Initialize
        document.addEventListener('DOMContentLoaded', function() {
            loadPools();
            
            // Set default date range (last 7 days)
            const endDate = new Date();
            const startDate = new Date();
            startDate.setDate(endDate.getDate() - 7);
            
            document.getElementById('startDate').value = startDate.toISOString().split('T')[0];
            document.getElementById('endDate').value = endDate.toISOString().split('T')[0];
            
            // Load initial data
            loadData();
        });
    </script>
</body>
</html>
"""
        self.send_response(200)
        self.send_header('Content-type', 'text/html')
        self.end_headers()
        self.wfile.write(html.encode())
    
    def get_pools(self):
        try:
            if os.path.exists('swm_pool_utility.db'):
                conn = sqlite3.connect('swm_pool_utility.db')
                cursor = conn.cursor()
                cursor.execute("SELECT DISTINCT name FROM track_pools WHERE name != '' ORDER BY name")
                pools = [row[0] for row in cursor.fetchall()]
                conn.close()
            else:
                pools = ["Dantebad", "Michaelibad", "Messebad"]  # Sample data
        except:
            pools = ["Dantebad", "Michaelibad", "Messebad"]  # Sample data
        
        self.send_response(200)
        self.send_header('Content-type', 'application/json')
        self.end_headers()
        self.wfile.write(json.dumps(pools).encode())
    
    def get_historical_data(self, parsed_path):
        query_params = parse_qs(parsed_path.query)
        
        try:
            if os.path.exists('swm_pool_utility.db'):
                conn = sqlite3.connect('swm_pool_utility.db')
                cursor = conn.cursor()
                
                # Build query
                sql = "SELECT name, dtime, utility FROM track_pools WHERE name != ''"
                params = []
                
                if 'pools' in query_params:
                    pools = query_params['pools'][0].split(',')
                    placeholders = ','.join(['?' for _ in pools])
                    sql += f" AND name IN ({placeholders})"
                    params.extend(pools)
                
                if 'start' in query_params:
                    sql += " AND date(dtime) >= ?"
                    params.append(query_params['start'][0])
                
                if 'end' in query_params:
                    sql += " AND date(dtime) <= ?"
                    params.append(query_params['end'][0])
                
                sql += " ORDER BY dtime"
                
                cursor.execute(sql, params)
                rows = cursor.fetchall()
                conn.close()
                
                # Process data
                data = {"labels": [], "datasets": []}
                pools_data = {}
                
                for name, dtime, utility in rows:
                    if name not in pools_data:
                        pools_data[name] = []
                    pools_data[name].append((dtime, utility))
                
                # Get all timestamps
                timestamps = set()
                for pool_data in pools_data.values():
                    for dtime, _ in pool_data:
                        timestamps.add(dtime.split(' ')[1][:5])  # Get time part
                
                data["labels"] = sorted(list(timestamps))
                
                # Create datasets
                colors = ['#FF6384', '#36A2EB', '#FFCE56', '#4BC0C0', '#9966FF']
                color_idx = 0
                
                for pool_name, pool_data in pools_data.items():
                    pool_times = {dtime.split(' ')[1][:5]: utility for dtime, utility in pool_data}
                    
                    dataset_data = []
                    for timestamp in data["labels"]:
                        dataset_data.append(pool_times.get(timestamp, 0))
                    
                    color = colors[color_idx % len(colors)]
                    data["datasets"].append({
                        "label": pool_name,
                        "data": dataset_data,
                        "borderColor": color,
                        "backgroundColor": color + "33"
                    })
                    color_idx += 1
            else:
                # Sample data
                data = {
                    "labels": ["08:00", "10:00", "12:00", "14:00", "16:00", "18:00"],
                    "datasets": [
                        {
                            "label": "Dantebad",
                            "data": [20, 35, 60, 80, 70, 45],
                            "borderColor": "#FF6384",
                            "backgroundColor": "#FF638433"
                        },
                        {
                            "label": "Michaelibad", 
                            "data": [15, 25, 45, 65, 55, 35],
                            "borderColor": "#36A2EB",
                            "backgroundColor": "#36A2EB33"
                        }
                    ]
                }
        except Exception as e:
            # Sample data on error
            data = {
                "labels": ["08:00", "10:00", "12:00", "14:00", "16:00", "18:00"],
                "datasets": [
                    {
                        "label": "Dantebad",
                        "data": [20, 35, 60, 80, 70, 45],
                        "borderColor": "#FF6384",
                        "backgroundColor": "#FF638433"
                    }
                ]
            }
        
        self.send_response(200)
        self.send_header('Content-type', 'application/json')
        self.end_headers()
        self.wfile.write(json.dumps(data).encode())

if __name__ == "__main__":
    PORT = 8080
    with socketserver.TCPServer(("", PORT), SWMHandler) as httpd:
        print(f"Starting web server on http://localhost:{PORT}")
        httpd.serve_forever()